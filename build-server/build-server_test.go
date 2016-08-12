package buildserver

import (
	"testing"
)

func TestAllocateOne(t *testing.T) {
	ids, err := Allocate(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("length not expected: %v", ids)
	}

	freed, err := Free(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if !freed {
		t.Fatal("build slave not freed")
	}
}
