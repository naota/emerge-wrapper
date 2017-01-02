package buildserver

import (
	"log"
	"net"

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

func main() {
	var numProcs uint32
	numProcs = 4
	b := newServer(numProcs)
	err := b.run(":50000")
	if err != nil {
		log.Fatal(err)
	}
}
