package main

import "testing"

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" kafka:9092, ,localhost:9092 ")
	want := []string{"kafka:9092", "localhost:9092"}
	if len(got) != len(want) {
		t.Fatalf("splitCSV length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitCSV[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
