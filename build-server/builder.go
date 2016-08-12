package buildserver

func main() {
	numProcs = 4

	builder := BuildServer{}

	serv := rpc.NewServer(builder, true, nil)
	l, err := net.Listen("tcp", "127.0.0.1:50000")
	if err != nil {
		log.Fatal(err)
	}

	serv.Listen(l)
	go serv.Run()
}
