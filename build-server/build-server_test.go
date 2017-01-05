package buildserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const testDir = "testdir"
const baseDir = "base"
const testDataDir = "testdata"
const baseFileName = "base.tar.xz"

func startServer(procs uint32) (*buildServer, BuildClient, *grpc.ClientConn, error) {
	addr := fmt.Sprintf(":%d", 10000+rand.Intn(30000))
	server := newServer(procs, testDir)
	go func() {
		err := server.run(addr)
		if err != nil {
			log.Printf("server: %v", err)
		}
	}()

	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, nil, err
	}
	client := NewBuildClient(conn)

	return server, client, conn, nil
}

type session struct {
	server *buildServer
	client BuildClient
	sid    sessionID
	conn   *grpc.ClientConn
}

func startSession(maxProcs, groupProcs uint32) (*session, error) {
	os.RemoveAll(testDir)
	server, client, conn, err := startServer(maxProcs)
	if err != nil {
		return nil, err
	}

	alloced, err := client.StartSession(context.Background(),
		&StartRequest{groupProcs})
	if err != nil {
		return nil, err
	}
	if alloced.NumBuilders != groupProcs {
		return nil, fmt.Errorf("#Workers not expected: %d != %d",
			alloced.NumBuilders, groupProcs)
	}
	sid := sessionID(alloced.SessionID)

	return &session{server, client, sid, conn}, nil
}

func closeSession(ses *session) error {
	if ses.sid != "" {
		freed, err := ses.client.CloseSession(context.Background(),
			&CloseRequest{string(ses.sid)})
		if err != nil {
			return err
		}
		if !freed.Freed {
			return fmt.Errorf("failed to free group")
		}
	}
	ses.conn.Close()
	ses.server.Stop()
	return nil
}

func TestAllocateOne(t *testing.T) {
	ses, err := startSession(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err := closeSession(ses)
		if err != nil {
			t.Fatal(err)
		}
	}()
}

func TestOverAllocate(t *testing.T) {
	ses, err := startSession(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = closeSession(ses)
		if err != nil {
			t.Fatal(err)
		}
	}()

	alloced, err := ses.client.StartSession(context.Background(), &StartRequest{1})
	if err != nil {
		t.Fatal(err)
	}
	if alloced.NumBuilders != 0 {
		t.Fatal("over allocated")
	}
}

func TestFreeNonExisting(t *testing.T) {
	ses, err := startSession(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = closeSession(ses)
		if err != nil {
			t.Fatal(err)
		}
	}()

	freed, err := ses.client.CloseSession(context.Background(), &CloseRequest{"NONEXIST"})
	if err != nil {
		t.Fatal(err)
	}
	if freed.Freed {
		t.Fatal("Free non-existent group")
	}
}

func TestSetupBase(t *testing.T) {
	ses, err := startSession(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = closeSession(ses)
		if err != nil {
			t.Fatal(err)
		}
	}()

	baseData, err := ioutil.ReadFile(filepath.Join(testDataDir, baseFileName))
	if err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256(baseData)
	testRoot := filepath.Join(testDir, baseDir, string(ses.sid))
	os.RemoveAll(testRoot)

	bdata := BaseData{
		SessionID:       string(ses.sid),
		ArchiveData:     baseData,
		ArchiveChecksum: checksum[:],
	}
	bres, err := ses.client.SetupBase(context.Background(), &bdata)
	if err != nil {
		t.Fatal(err)
	}
	if !bres.Succeed {
		t.Fatal("not succed w/ good checksum", bres.Error)
	}

	_, err = os.Stat(filepath.Join(testRoot, "testfile"))
	if os.IsNotExist(err) {
		t.Fatal("test root not unpacked:", err)
	}

	// unpack same dir
	bres, err = ses.client.SetupBase(context.Background(), &bdata)
	if err != nil {
		t.Fatal(err)
	}
	if bres.Error != BaseResponse_BaseExists {
		t.Fatal("expected existing base error, got", bres.Error)
	}

	// wrong checksum data
	bdata.ArchiveChecksum = make([]byte, sha256.Size)
	bres, err = ses.client.SetupBase(context.Background(), &bdata)
	if err != nil {
		t.Fatal(err)
	}
	if bres.Error != BaseResponse_ChecksumNotMatch {
		t.Fatal("expected bad checksum error")
	}

	// wrong checksum size
	bdata.ArchiveChecksum = make([]byte, 4)
	bres, err = ses.client.SetupBase(context.Background(), &bdata)
	if err != nil {
		t.Fatal(err)
	}
	if bres.Error != BaseResponse_BadChecksumSize {
		t.Fatal("expected checksum size error")
	}
}

func TestCheckPackages(t *testing.T) {
	ses, err := startSession(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = closeSession(ses)
		if err != nil {
			t.Fatal(err)
		}
	}()

	baseData, err := ioutil.ReadFile(filepath.Join(testDataDir, baseFileName))
	if err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256(baseData)
	testRoot := filepath.Join(testDir, baseDir, hex.EncodeToString(checksum[:]))
	os.RemoveAll(testRoot)

	bres, err := ses.client.SetupBase(context.Background(),
		&BaseData{string(ses.sid), baseData, checksum[:]})
	if err != nil {
		t.Fatal(err)
	}
	if !bres.Succeed {
		t.Fatal("not succed w/ good checksum", bres.Error)
	}

	md := metadata.Pairs("sid", string(ses.sid))
	ctx := metadata.NewContext(context.Background(), md)
	stream, err := ses.client.CheckPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}

	dummycpv := "test-xxx/dummy-0"
	//invalidcpv := "invalid"
	err = stream.Send(&Package{dummycpv, checksum[:]})
	if err != nil {
		t.Fatal(err)
	}
	pkgreq, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgreq.GetPkg()
	if pkg == nil {
		t.Fatal("expected package request:", pkgreq.GetError())
	}
	if pkg.Cpv != dummycpv {
		t.Fatal("expected dummy package request, got", pkg.Cpv)
	}

	pkgData := baseData
	dres, err := ses.client.DeployPackage(context.Background(),
		&DeployInfo{string(ses.sid), pkg, pkgData})
	if err != nil {
		t.Fatal(err)
	}
	if dres.Error != DeployResponse_NoError {
		t.Fatal(dres.Error)
	}

	// send the same package request
	err = stream.Send(&Package{dummycpv, checksum[:]})
	if err != nil {
		t.Fatal(err)
	}
	err = stream.CloseSend()
	if err != nil {
		t.Fatal(err)
	}
	pkgreq, err = stream.Recv()
	if err != io.EOF {
		t.Fatal("expected EOF:", pkgreq, err)
	}

	// without valid context
	stream, err = ses.client.CheckPackages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pkgreq, err = stream.Recv()
	if pkgreq.GetError() != PackageRequest_InvalidRequest {
		t.Fatal("expexted invalid error")
	}
	stream.CloseSend()

	// with multiple sids
	md = metadata.Pairs("sid", string(ses.sid), "sid", "dummy")
	ctx = metadata.NewContext(context.Background(), md)
	stream, err = ses.client.CheckPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pkgreq, err = stream.Recv()
	if pkgreq.GetError() != PackageRequest_InvalidRequest {
		t.Fatal("expexted invalid error")
	}
	stream.CloseSend()

	// with multiple sids
	md = metadata.Pairs("sid", "dummy")
	ctx = metadata.NewContext(context.Background(), md)
	stream, err = ses.client.CheckPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pkgreq, err = stream.Recv()
	if pkgreq.GetError() != PackageRequest_NoBase {
		t.Fatal("expexted no base error")
	}
	stream.CloseSend()
}
