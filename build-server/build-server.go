//go:generate stringer -type=BuilderError

package buildserver

import (
	"fmt"
	"github.com/msgpack-rpc/msgpack-rpc-go/rpc"
	"github.com/satori/go.uuid"
	"log"
	"net"
	"reflect"
)

type BuilderID string
type BuilderInfo struct {
	id BuilderID
}

type BuildServer map[string]reflect.Value

type BuilderError int

const (
	NoSuchBuilder BuilderError = iota
)

var numProcs uint
var builders map[BuilderID]BuilderInfo

func (builder BuildServer) Resolve(name string, arguments []reflect.Value) (reflect.Value, error) {
	return builder[name], nil
}

func Allocate(cnt uint) ([]BuilderID, fmt.Stringer) {
	n := cnt
	if cnt > numProcs {
		n = numProcs
	}
	numProcs -= n

	var i uint
	var ids []BuilderID
	for i = 0; i < n; i++ {
		b := newBuilder()
		builders[b.id] = b
		ids = append(ids, b.id)
	}

	return ids, nil
}

func newBuilder() BuilderInfo {
	b := BuilderInfo{}
	b.id = BuilderID(uuid.NewV4().String())
	return b
}

func Free(id BuilderID) (bool, fmt.Stringer) {
	_, ok := builders[id]
	if !ok {
		return false, NoSuchBuilder
	}
	delete(builders, id)
	return true, nil
}
