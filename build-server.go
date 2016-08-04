package main

import (
	"github.com/msgpack-rpc/msgpack-rpc-go/rpc"
	"log"
	"net"
	"reflect"
)

type BuildServer map[string]reflect.Value

func (builder BuildServer) Resolve(name string, arguments []reflect.Value) (reflect.Value, error) {
	return builder[name], nil
}

func main() {
	builder := BuildServer{}

	serv := rpc.NewServer(builder, true, nil)
	l, err := net.Listen("tcp", "127.0.0.1:50000")
	if err != nil {
		log.Fatal(err)
	}

	serv.Listen(l)
	go serv.Run()
}
