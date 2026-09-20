package people

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"0912 345 678":    "0912345678",
		"0912-345-678":    "0912345678",
		"0912.345.678":    "0912345678",
		"0912345678":      "0912345678",
		"+84 912 345 678": "+84912345678", // + kept (pymongo port strips [\s.\-] only)
	}
	for in, want := range cases {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractNumbers(t *testing.T) {
	// CCCD: only 9- or 12-digit groups count.
	got := extractNumbers("cccd 012345678901 hoặc 123456789, năm 2020", []int{9, 12}, 0)
	require.Equal(t, []string{"012345678901", "123456789"}, got)

	// Phone: exactly 10 digits, separators inside a group merge it first.
	got = extractNumbers("gọi 0912 345 678 hay 0987.654.321", []int{10}, 0)
	require.Equal(t, []string{"0912345678", "0987654321"}, got)

	// BHXH: min length 5, dedup.
	got = extractNumbers("bhxh 12345 lại 12345 và 1234567890", nil, 5)
	require.Equal(t, []string{"12345", "1234567890"}, got)

	// Short groups rejected.
	got = extractNumbers("số 123 quá ngắn", nil, 5)
	require.Empty(t, got)
}

func TestLimitFor(t *testing.T) {
	require.Equal(t, int64(10), limitFor(1, 10))
	require.Equal(t, int64(30), limitFor(3, 10))
	require.Equal(t, int64(100), limitFor(20, 10)) // hard cap
}

func TestIdentityKey(t *testing.T) {
	require.Equal(t, "cccd:012345678901",
		identityKey(map[string]string{"cccd": "012345678901"}, "x"))
	require.Equal(t, "bhxh:7777777",
		identityKey(map[string]string{"bhxh": "7777777"}, "x"))
	// phone alone must never merge people — falls through to name+extra.
	require.Equal(t, "np:nguyen van a|1985",
		identityKey(map[string]string{"name": "Nguyen Van A", "dob": "1985", "phone": "0912345678"}, "x"))
	require.Equal(t, "id:doc-1",
		identityKey(map[string]string{}, "doc-1"))
}

func d(pairs ...interface{}) bson.D {
	doc := bson.D{}
	for i := 0; i+1 < len(pairs); i += 2 {
		doc = append(doc, bson.E{Key: pairs[i].(string), Value: pairs[i+1]})
	}
	return doc
}

func TestConsolidate_GroupsSamePersonAcrossSchemas(t *testing.T) {
	results := map[string][]bson.D{
		"bhxh": {d("hoTen", "NGUYEN VAN A", "soCmnd", "012345678901", "maSoBhxh", "7777777")},
		"lg":   {d("TenHoiVien", "Nguyen Van A", "SoDinhDanh", "012345678901")},
	}
	persons, display, schemas := consolidate(results, "cccd")
	// persons is flat (one record per source doc) — same CCCD shares one
	// _person_group, which is what merges them into a single profile block.
	require.Len(t, persons, 2)
	require.Equal(t, persons[0]["_person_group"], persons[1]["_person_group"])
	require.Contains(t, display, "NGUYEN VAN A")
	require.ElementsMatch(t, []string{"bhxh", "lg"}, schemas)
}

func TestConsolidate_PhoneAloneDoesNotMerge(t *testing.T) {
	// Same phone, different names → two distinct profiles.
	results := map[string][]bson.D{
		"cv19": {
			d("ho_ten", "NGUYEN VAN A", "so_dien_thoai", "0912345678"),
			d("ho_ten", "TRAN THI B", "so_dien_thoai", "0912345678"),
		},
	}
	persons, _, _ := consolidate(results, "phone")
	require.Len(t, persons, 2)
	require.NotEqual(t, persons[0]["_person_group"], persons[1]["_person_group"],
		"two people sharing a phone number must stay two profiles")
}

func TestConsolidate_SanitizesObjectIDs(t *testing.T) {
	oid := bson.NewObjectID()
	results := map[string][]bson.D{
		"bhxh": {d("_id", oid, "hoTen", "LE VAN C", "maSoBhxh", "12345",
			"ref", oid.Hex())},
	}
	persons, display, _ := consolidate(results, "bhxh")
	require.Len(t, persons, 1)
	for k, v := range persons[0] {
		if isObjectID(v) {
			t.Errorf("field %s leaked an ObjectId", k)
		}
	}
	if strings.Contains(display, oid.Hex()) {
		t.Errorf("display leaked the ObjectId hex")
	}
}

func TestConsolidate_Empty(t *testing.T) {
	persons, display, schemas := consolidate(map[string][]bson.D{}, "cccd")
	require.Empty(t, persons)
	require.Empty(t, display)
	require.Empty(t, schemas)
}

func TestStripObjectIDText(t *testing.T) {
	in := "id [0123456789abcdef01234567] bare 0123456789abcdef01234567 short abc123"
	got := stripObjectIDText(in)
	if strings.Contains(got, "0123456789abcdef") {
		t.Errorf("ObjectId text not stripped: %q", got)
	}
	if !strings.Contains(got, "abc123") {
		t.Errorf("non-ObjectId text removed: %q", got)
	}
}

func TestService_DisabledFailsFast(t *testing.T) {
	svc := NewService(Config{Enabled: false})
	require.False(t, svc.Enabled())
	// Disabled → database() fails fast, no connection attempt, so the caller
	// sees "unavailable" rather than a multi-second hang or a false empty.
	res := svc.SearchByName(context.Background(), "Nguyen Van A", 10)
	require.NotNil(t, res)
	require.True(t, res.Unavailable)
}

func TestService_UnreachableMongoIsBusyNotEmpty(t *testing.T) {
	svc := NewService(Config{
		Enabled:      true,
		Host:         "127.0.0.1",
		Port:         1, // unreachable
		Database:     "local",
		QueryTimeout: 500 * time.Millisecond,
	})
	require.True(t, svc.Enabled())
	res := svc.SearchByCCCD(context.Background(), "012345678901")
	require.NotNil(t, res)
	require.False(t, res.Found)
	require.True(t, res.Unavailable, "dead MongoDB must be 'busy', not 'not found'")
	require.Contains(t, res.Display, "bận")
}

func TestSearchByCCCD_InvalidInput(t *testing.T) {
	svc := NewService(Config{Enabled: false})
	res := svc.SearchByCCCD(context.Background(), "abc không có số")
	require.False(t, res.Found)
	require.Contains(t, res.Display, "9 hoặc 12")
}

func TestSchemaMapSanity(t *testing.T) {
	for _, lt := range []string{"cccd", "bhxh", "phone", "name", "advanced"} {
		cfg, ok := SearchableCollectionMap[lt]
		require.True(t, ok, "missing lookup type %s", lt)
		require.NotEmpty(t, cfg.Collections)
	}
	// The lg phone index lives on "phone", not "SoDienThoai" (AIRAG comment).
	require.Equal(t, []string{"phone"},
		SearchableCollectionMap["phone"].Collections["lg"].Fields)
}
