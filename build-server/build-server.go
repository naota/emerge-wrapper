package buildserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/satori/go.uuid"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type groupID string
type buildGroup struct {
	id            groupID
	maxBuilders   uint32
	usingBuilders uint32
}

type buildServer struct {
	rpcServer *grpc.Server
	numProcs  uint32
	groups    map[groupID]buildGroup
	workdir   string
	baseDir   string
	binPkgDir string
	cacheDir  string
	tmpDir    string
}

func newServer(numProcs uint32, workdir string) *buildServer {
	b := buildServer{
		rpcServer: nil,
		numProcs:  numProcs,
		groups:    map[groupID]buildGroup{},
		workdir:   workdir,
		baseDir:   filepath.Join(workdir, "base"),
		binPkgDir: filepath.Join(workdir, "binpkgs"),
		cacheDir:  filepath.Join(workdir, "cache"),
		tmpDir:    filepath.Join(workdir, "tmp"),
	}

	for _, d := range []string{b.baseDir, b.binPkgDir, b.cacheDir, b.tmpDir} {
		os.MkdirAll(d, 0700)
	}

	return &b
}

func (server *buildServer) run(laddr string) error {
	lis, err := net.Listen("tcp", laddr)
	if err != nil {
		return err
	}

	rpcServer := grpc.NewServer()
	RegisterBuildServer(rpcServer, server)
	server.rpcServer = rpcServer
	return rpcServer.Serve(lis)
}

func (server *buildServer) tempFile(prefix string) (*os.File, error) {
	return ioutil.TempFile(server.tmpDir, prefix)
}

func (server *buildServer) Stop() {
	server.rpcServer.GracefulStop()
}

func (server *buildServer) AllocateGroup(ctx context.Context, req *AllocationRequest) (*AllocationResponse, error) {
	n := req.NumProcs
	if n > server.numProcs {
		n = server.numProcs
	}
	server.numProcs -= n

	g := newGroup(n)
	server.groups[g.id] = g

	return &AllocationResponse{n, string(g.id)}, nil
}

func newGroup(n uint32) buildGroup {
	b := buildGroup{}
	b.id = groupID(uuid.NewV4().String())
	b.maxBuilders = n
	b.usingBuilders = 0
	return b
}

func (server *buildServer) FreeGroup(ctx context.Context, req *FreeRequest) (*FreeResponse, error) {
	id := groupID(req.GroupId)
	_, ok := server.groups[id]
	if !ok {
		return &FreeResponse{false}, nil
	}
	delete(server.groups, id)
	return &FreeResponse{true}, nil
}

func (server *buildServer) SetupBase(ctx context.Context, baseInfo *BaseData) (*BaseResponse, error) {
	const size = sha256.Size

	gid := string(baseInfo.GroupId)
	data := baseInfo.ArchiveData

	if len(baseInfo.ArchiveChecksum) != size {
		return &BaseResponse{false, BaseResponse_BadChecksumSize}, nil
	}
	var csum [size]byte
	copy(csum[:], baseInfo.ArchiveChecksum)

	if sha256.Sum256(data) != csum {
		return &BaseResponse{false, BaseResponse_ChecksumNotMatch}, nil
	}

	tmpfile, err := server.tempFile("archive")
	if err != nil {
		log.Print(err)
		return &BaseResponse{false, BaseResponse_InternalError}, nil
	}
	defer os.Remove(tmpfile.Name())

	if _, err = tmpfile.Write(data); err != nil {
		log.Print(err)
		return &BaseResponse{false, BaseResponse_InternalError}, nil
	}
	if err = tmpfile.Close(); err != nil {
		log.Print(err)
		return &BaseResponse{false, BaseResponse_InternalError}, nil
	}

	dir := filepath.Join(server.baseDir, gid)
	_, err = os.Stat(dir)
	if err == nil {
		return &BaseResponse{false, BaseResponse_BaseExists}, nil
	}

	os.MkdirAll(dir, 0700)
	err = exec.Command("tar", "-Jxf", tmpfile.Name(), "-C", dir).Run()
	if err != nil {
		log.Print(err)
		return &BaseResponse{false, BaseResponse_BadArchive}, nil
	}

	return &BaseResponse{true, BaseResponse_NoError}, nil
}

func getGroupFromContext(ctx context.Context) (groupID, bool) {
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return "", false
	}
	gids, ok := md["gid"]
	if !ok {
		return "", false
	}
	if len(gids) != 1 {
		return "", false
	}
	return groupID(gids[0]), true
}

func (server *buildServer) CheckPackages(stream Build_CheckPackagesServer) error {
	sendError := func(errCode PackageRequest_ErrorCode) {
		stream.Send(&PackageRequest{&PackageRequest_Error{errCode}})
	}
	request := func(pkg *Package) {
		stream.Send(&PackageRequest{&PackageRequest_Pkg{pkg}})
	}

	gid, ok := getGroupFromContext(stream.Context())
	if !ok {
		sendError(PackageRequest_InvalidRequest)
		return nil
	}

	_, ok = server.groups[gid]
	if !ok {
		sendError(PackageRequest_NoBase)
		return nil
	}

	for {
		pkg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Print(err)
			sendError(PackageRequest_NetworkError)
			return nil
		}
		haspkg, err := server.hasPackageCache(pkg)
		if err != nil {
			log.Print(err)
			sendError(PackageRequest_InternalError)
			return nil
		}
		if !haspkg {
			request(pkg)
		} else {
			server.linkPackage(gid, pkg)
		}
	}

	return nil
}

func (server *buildServer) linkPackage(gid groupID, pkg *Package) error {
	cacheName, err := cacheFileName(pkg)
	if err != nil {
		return err
	}
	pkgName, err := pkgFileName(pkg)
	if err != nil {
		return err
	}

	cachefile := filepath.Join(server.cacheDir, cacheName)
	pkgfile := filepath.Join(server.binPkgDir, string(gid), pkgName)
	os.MkdirAll(filepath.Dir(pkgfile), 0700)
	return os.Symlink(cachefile, pkgfile)
}

func validCPV(cpv string) bool {
	parts := strings.Split(cpv, "/")
	if len(parts) != 2 {
		return false
	}
	return true
}

func pkgFileName(pkg *Package) (string, error) {
	if !validCPV(pkg.Cpv) {
		return "", fmt.Errorf("invalid CPV")
	}
	parts := strings.Split(pkg.Cpv, "/")
	return filepath.Join(parts[0], parts[1]+".tbz2"), nil
}

func cacheFileName(pkg *Package) (string, error) {
	if len(pkg.Checksum) != sha256.Size {
		return "", fmt.Errorf("invalid checksum")
	}

	hexstr := hex.EncodeToString(pkg.Checksum)
	return hexstr + ".tbz2", nil
}

func (server *buildServer) hasPackageCache(pkg *Package) (bool, error) {
	name, err := cacheFileName(pkg)
	if err != nil {
		log.Print(err)
		return false, err
	}

	_, err = os.Stat(filepath.Join(server.cacheDir, name))
	switch {
	case os.IsNotExist(err):
		return false, nil
	case err == nil:
		return true, nil
	default:
		return false, err
	}
}

func verifyPackageFile(pkg *Package, tmpfile *os.File) bool {
	hash := sha256.New()
	tmpfile.Seek(0, os.SEEK_SET)
	_, err := io.Copy(hash, tmpfile)
	if err != nil {
		log.Print(err)
		return false
	}

	return bytes.Equal(hash.Sum(nil), pkg.Checksum)
}

func (server *buildServer) DeployPackage(ctx context.Context, info *DeployInfo) (*DeployResponse, error) {
	gid := groupID(info.GroupId)
	pkg := info.PkgInfo
	data := info.Data

	cacheName, err := cacheFileName(pkg)
	if err != nil {
		return &DeployResponse{DeployResponse_BadChecksum}, nil
	}
	cacheFile := filepath.Join(server.cacheDir, cacheName)

	tmpfile, err := server.tempFile("pkg")
	if err != nil {
		return &DeployResponse{DeployResponse_InternalError}, nil
	}
	tmpname := tmpfile.Name()
	defer os.Remove(tmpname)
	tmpfile.Write(data)

	if !verifyPackageFile(pkg, tmpfile) {
		return &DeployResponse{DeployResponse_InvalidPackage}, nil
	}

	err = os.Rename(tmpfile.Name(), cacheFile)
	if err != nil {
		log.Print(err)
		return &DeployResponse{DeployResponse_InternalError}, nil
	}

	err = server.linkPackage(gid, pkg)
	if err != nil {
		return &DeployResponse{DeployResponse_InternalError}, nil
	}

	return &DeployResponse{}, nil
}
