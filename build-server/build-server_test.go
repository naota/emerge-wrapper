package buildserver

import (
	"crypto/sha256"
	"fmt"
	"log"
	"math/rand"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

func startServer() (*buildServer, BuildClient, *grpc.ClientConn, error) {
	addr := fmt.Sprintf(":%d", 10000+rand.Intn(30000))
	server := newServer(1)
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

func TestAllocateOne(t *testing.T) {
	server, client, conn, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer server.Stop()

	alloced, err := client.AllocateGroup(context.Background(), &AllocationRequest{1})
	if err != nil {
		t.Fatal(err)
	}
	if alloced.NumBuilders != 1 {
		t.Fatalf("#Workers not expected: %v", alloced.NumBuilders)
	}

	freed, err := client.FreeGroup(context.Background(), &FreeRequest{alloced.GroupId})
	if err != nil {
		t.Fatal(err)
	}
	if !freed.Freed {
		t.Fatal("build slave not freed")
	}
}

func TestOverAllocate(t *testing.T) {
	server, client, conn, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer server.Stop()

	alloced, err := client.AllocateGroup(context.Background(), &AllocationRequest{2})
	if err != nil {
		t.Fatal(err)
	}
	if alloced.NumBuilders != 1 {
		t.Fatal("over allocation")
	}
}

func TestFreeNonExisting(t *testing.T) {
	server, client, conn, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer server.Stop()

	freed, err := client.FreeGroup(context.Background(), &FreeRequest{"NONEXIST"})
	if err != nil {
		t.Fatal(err)
	}
	if freed.Freed {
		t.Fatal("Free non-existent group")
	}
}

func TestSetupBase(t *testing.T) {
	server, client, conn, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer server.Stop()

	sinfo, err := client.AllocateGroup(context.Background(), &AllocationRequest{1})
	if err != nil {
		t.Fatal(err)
	}

	gid := sinfo.GroupId
	defer client.FreeGroup(context.Background(), &FreeRequest{gid})

	baseData := []byte{0, 1, 2, 3}
	checksum := sha256.Sum256(baseData)
	bdata := BaseData{
		ArchiveData:     baseData,
		ArchiveChecksum: make([]byte, sha256.Size),
	}
	copy(bdata.ArchiveChecksum, checksum[:])
	bres, err := client.SetupBase(context.Background(), &bdata)
	if err != nil {
		t.Fatal(err)
	}
	if !bres.Succeed {
		t.Fatal("not succed w/ good checksum")
	}

	bdata.ArchiveChecksum = make([]byte, sha256.Size)
	bres, err = client.SetupBase(context.Background(), &bdata)
	if err != nil {
		t.Fatal(err)
	}
	if bres.Error != BaseResponse_ChecksumNotMatch {
		t.Fatal("expected bad checksum error")
	}

	bdata.ArchiveChecksum = make([]byte, 4)
	bres, err = client.SetupBase(context.Background(), &bdata)
	if err != nil {
		t.Fatal(err)
	}
	if bres.Error != BaseResponse_BadChecksumSize {
		t.Fatal("expected checksum size error")
	}
}
