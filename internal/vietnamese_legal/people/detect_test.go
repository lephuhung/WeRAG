package people

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectLookupRequest(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		wantMatch        bool
		lookupType       string
		query            string
		limit            int64
		continuePipeline bool
	}{
		{name: "phone with intent", input: "Tra cứu số điện thoại 0989755968", wantMatch: true, lookupType: "phone", query: "0989755968"},
		{name: "spaced phone belongs-to", input: "0989 755 968 là của ai?", wantMatch: true, lookupType: "phone", query: "0989755968"},
		{name: "cccd keyword", input: "Tra cứu CCCD 012345678901", wantMatch: true, lookupType: "cccd", query: "012345678901"},
		{name: "bhxh precedes phone", input: "Kiểm tra BHXH 1234567890", wantMatch: true, lookupType: "bhxh", query: "1234567890"},
		{name: "name lookup", input: "Tra cứu người tên Nguyễn Văn A", wantMatch: true, lookupType: "name", query: "Nguyễn Văn A", limit: 10},
		{name: "name lookup trailing period", input: "Tra cứu người tên Nguyễn Văn A.", wantMatch: true, lookupType: "name", query: "Nguyễn Văn A", limit: 10},
		{name: "bare number", input: "0989755968", wantMatch: true, lookupType: "phone", query: "0989755968"},
		{name: "multi-intent phone plus legal", input: "Tìm thông tin số điện thoại 0989755968 và đối chiếu quy định về hồ sơ cấp độ xem vi phạm gì không", wantMatch: true, lookupType: "phone", query: "0989755968", continuePipeline: true},
		{name: "phone term without intent", input: "Quy định bảo mật số điện thoại 0989755968", wantMatch: false},
		{name: "document search not a lookup", input: "Tìm tài liệu về Nguyễn Văn A", wantMatch: false},
		{name: "empty", input: "", wantMatch: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, ok := DetectLookupRequest(tc.input)
			if !tc.wantMatch {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.Equal(t, tc.lookupType, req.LookupType)
			assert.Equal(t, tc.query, req.Query)
			assert.Equal(t, tc.continuePipeline, req.ContinuePipeline)
			if tc.lookupType == "name" {
				assert.Equal(t, int64(10), req.Limit)
			} else if tc.limit != 0 {
				assert.Equal(t, tc.limit, req.Limit)
			}
		})
	}
}
