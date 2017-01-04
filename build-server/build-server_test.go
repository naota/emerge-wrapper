package buildserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const baseDir = "base"
const testDataDir = "testdata"

func startServer(procs uint32) (*buildServer, BuildClient, *grpc.ClientConn, error) {
	addr := fmt.Sprintf(":%d", 10000+rand.Intn(30000))
	server := newServer(procs)
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
	gid    groupID
	conn   *grpc.ClientConn
}

func startSession(maxProcs, groupProcs uint32) (*session, error) {
	server, client, conn, err := startServer(maxProcs)
	if err != nil {
		return nil, err
	}

	alloced, err := client.AllocateGroup(context.Background(),
		&AllocationRequest{groupProcs})
	if err != nil {
		return nil, err
	}
	if alloced.NumBuilders != groupProcs {
		return nil, fmt.Errorf("#Workers not expected: %d != %d",
			alloced.NumBuilders, groupProcs)
	}
	gid := groupID(alloced.GroupId)

	return &session{server, client, gid, conn}, nil
}

func closeSession(ses *session) error {
	if ses.gid != "" {
		freed, err := ses.client.FreeGroup(context.Background(),
			&FreeRequest{string(ses.gid)})
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

	alloced, err := ses.client.AllocateGroup(context.Background(), &AllocationRequest{1})
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

	freed, err := ses.client.FreeGroup(context.Background(), &FreeRequest{"NONEXIST"})
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

	baseData, err := ioutil.ReadFile(filepath.Join(testDataDir, "test.tar.xz"))
	if err != nil {
		t.Fatal(err)
	}
	checksum := sha256.Sum256(baseData)
	testRoot := filepath.Join(baseDir, hex.EncodeToString(checksum[:]))
	os.RemoveAll(testRoot)

	bdata := BaseData{
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

	_, err = os.Open(filepath.Join(testRoot, "testfile"))
	if os.IsNotExist(err) {
		t.Fatal("test root not unpacked:", err)
	}

	// unpack same dir
	bres, err = ses.client.SetupBase(context.Background(), &bdata)
	if err != nil {
		t.Fatal(err)
	}
	if !bres.Succeed {
		t.Fatal("not succed w/ good checksum", bres.Error)
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
