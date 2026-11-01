package types

import "testing"

func TestToConnectionMintsAnIDUnlessGiven(t *testing.T) {
	minted := (&CreateConnectionRequest{NodeID: "n", Name: "c"}).ToConnection()
	if minted.ID == "" {
		t.Fatal("expected a generated id")
	}

	given := (&CreateConnectionRequest{NodeID: "n", Name: "c", ID: "conn_queued"}).ToConnection()
	if given.ID != "conn_queued" {
		t.Fatalf("expected the caller's id to be kept, got %q", given.ID)
	}
}
