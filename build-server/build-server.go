package buildserver

import (
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/satori/go.uuid"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
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
}

func newServer(numProcs uint32) *buildServer {
	b := new(buildServer)
	b.numProcs = numProcs
	b.groups = map[groupID]buildGroup{}

	return b
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
	const tmpDir = "tmp"
	const baseDir = "base"

	data := baseInfo.ArchiveData

	if len(baseInfo.ArchiveChecksum) != size {
		return &BaseResponse{false, BaseResponse_BadChecksumSize}, nil
	}
	var csum [size]byte
	copy(csum[:], baseInfo.ArchiveChecksum)

	if sha256.Sum256(data) != csum {
		return &BaseResponse{false, BaseResponse_ChecksumNotMatch}, nil
	}

	os.MkdirAll(tmpDir, 0700)
	tmpfile, err := ioutil.TempFile(tmpDir, "archive")
	if err != nil {
		log.Print(err)
		return &BaseResponse{false, BaseResponse_OtherError}, nil
	}
	defer os.Remove(tmpfile.Name())

	if _, err = tmpfile.Write(data); err != nil {
		log.Print(err)
		return &BaseResponse{false, BaseResponse_OtherError}, nil
	}
	if err = tmpfile.Close(); err != nil {
		log.Print(err)
		return &BaseResponse{false, BaseResponse_OtherError}, nil
	}

	dir := filepath.Join(baseDir, hex.EncodeToString(csum[:]))
	_, err = os.Open(dir)
	if os.IsNotExist(err) {
		os.MkdirAll(dir, 0700)
		err = exec.Command("tar", "-Jxf", tmpfile.Name(), "-C", dir).Run()
		if err != nil {
			log.Print(err)
			return &BaseResponse{false, BaseResponse_BadArchive}, nil
		}
	} else {
		now := time.Now()
		err = os.Chtimes(dir, now, now)
		if err != nil {
			log.Print(err)
		}
	}

	return &BaseResponse{true, BaseResponse_NoError}, nil
}
