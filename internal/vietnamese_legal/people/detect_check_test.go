package people

import (
	"fmt"
	"testing"
)

func TestCheckSpecificQuery(t *testing.T) {
	queries := []string{
		"Thông tin cá nhân cơ bản gồm gì",
		"Thông tin cá nhân cơ bản gồm gì?",
		"thông tin cá nhân cơ bản gồm những gì",
		"Thông tin cá nhân là gì",
	}
	for _, q := range queries {
		req, ok := DetectLookupRequest(q)
		fmt.Printf("query=%q ok=%v req=%+v\n", q, ok, req)
	}
}
