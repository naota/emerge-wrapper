//go:generate stringer -type=BuildServerError

package buildserver

import (
	"fmt"
	"github.com/msgpack-rpc/msgpack-rpc-go/rpc"
	"github.com/satori/go.uuid"
	"net"
	"reflect"
)

type GroupID string
type BuildGroup struct {
	id            GroupID
	maxBuilders   uint
	usingBuilders uint
}

type GroupInfo struct {
	ID          GroupID
	NumBuilders uint
}

type builderFuncMap map[string]reflect.Value
type BuildServer struct {
	funcMap  builderFuncMap
	numProcs uint
	server   *rpc.Server
	listener net.Listener
	groups   map[GroupID]BuildGroup
}

type BuildServerError int

const (
	NoSuchGroup BuildServerError = iota
)

func NewServer(numProcs uint) *BuildServer {
	b := new(BuildServer)
	b.funcMap = builderFuncMap{
		"allocate": reflect.ValueOf(BuildServer.AllocateGroup),
		"free":     reflect.ValueOf(BuildServer.FreeGroup),
	}
	b.numProcs = numProcs
	b.groups = map[GroupID]BuildGroup{}

	return b
}

func (server BuildServer) Run(laddr string) error {
	var err error
	server.server = rpc.NewServer(server.funcMap, true, nil)
	server.listener, err = net.Listen("tcp", laddr)
	if err != nil {
		return err
	}
	server.server.Run()
	return nil
}

func (builder builderFuncMap) Resolve(name string, arguments []reflect.Value) (reflect.Value, error) {
	return builder[name], nil
}

func (server BuildServer) AllocateGroup(cnt uint) (GroupInfo, fmt.Stringer) {
	n := cnt
	if cnt > server.numProcs {
		n = server.numProcs
	}
	server.numProcs -= n

	var ids []GroupID
	g := newGroup(n)
	server.groups[g.id] = g
	ids = append(ids, g.id)

	gi := GroupInfo{}
	gi.ID = g.id
	gi.NumBuilders = n

	return gi, nil
}

func newGroup(n uint) BuildGroup {
	b := BuildGroup{}
	b.id = GroupID(uuid.NewV4().String())
	b.maxBuilders = n
	b.usingBuilders = 0
	return b
}

func (server BuildServer) FreeGroup(id GroupID) (bool, fmt.Stringer) {
	_, ok := server.groups[id]
	if !ok {
		return false, NoSuchGroup
	}
	delete(server.groups, id)
	return true, nil
}
