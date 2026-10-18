package handler

import "testing"

func TestSameDocReaderHost(t *testing.T) {
	cases := []struct {
		addr, configured string
		want             bool
	}{
		{"docreader:50051", "docreader:50051", true},
		{"docreader", "docreader:50051", true},
		{"http://docreader:8080/", "docreader:50051", true},
		{"DOCREADER:50051", "docreader:50051", true},
		{"other:50051", "docreader:50051", false},
		{"", "docreader:50051", false},
		{"docreader:50051", "", false},
	}
	for _, c := range cases {
		if got := sameDocReaderHost(c.addr, c.configured); got != c.want {
			t.Errorf("sameDocReaderHost(%q, %q) = %v, want %v", c.addr, c.configured, got, c.want)
		}
	}
}
