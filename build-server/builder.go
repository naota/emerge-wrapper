package buildserver

import (
	"log"
)

func main() {
	var numProcs uint
	numProcs = 4
	b := NewServer(numProcs)
	err := b.Run("127.0.0.1:50000")
	if err != nil {
		log.Fatal(err)
	}
}
