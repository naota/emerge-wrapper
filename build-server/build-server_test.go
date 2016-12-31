package buildserver

import (
	"log"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

func startServer() (*buildServer, BuildClient, *grpc.ClientConn, error) {
	addr := ":50000"
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
