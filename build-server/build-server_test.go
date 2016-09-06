package buildserver

import (
	"testing"
)

func TestAllocateOne(t *testing.T) {
	server := NewServer(1)

	ginfo, err := server.AllocateGroup(1)
	if err != nil {
		t.Fatal(err)
	}
	if ginfo.NumBuilders != 1 {
		t.Fatalf("#Workers not expected: %v", ginfo.NumBuilders)
	}

	freed, err := server.FreeGroup(ginfo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !freed {
		t.Fatal("build slave not freed")
	}
}
