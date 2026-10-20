package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func legalNameTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.LegalDocument{}, &types.LegalDocumentRelation{}))
	day := func(s string) *time.Time {
		if s == "" {
			return nil
		}
		d, _ := time.Parse("2006-01-02", s)
		return &d
	}
	doc := func(id, num, title, issued, eff, exp, status string) *types.LegalDocument {
		typ := "Luật"
		if strings.HasPrefix(title, "Pháp lệnh") {
			typ = "Pháp lệnh"
		}
		return &types.LegalDocument{ID: id, Source: types.LegalDocumentSourceVBPL, DocumentNumber: num,
			NumberKey: vietnamese_legal.CatalogNumberKey(num), DocType: typ, Title: title, Status: status,
			IssuedOn: day(issued), EffectiveOn: day(eff), ExpiredOn: day(exp)}
	}
	// as on dev: 24/2018 still "Còn hiệu lực" with no expiry date
	catalog := repository.NewLegalCatalogRepository(db)
	require.NoError(t, catalog.UpsertDocuments(context.Background(), []*types.LegalDocument{
		doc("132957", "24/2018/QH14", "Luật An ninh mạng số 24/2018/QH14", "2018-06-12", "2019-01-01", "", "Còn hiệu lực"),
		doc("187039", "116/2025/QH15", "LUẬT AN NINH MẠNG SỐ 116/2025/QH15", "2025-12-10", "2026-07-01", "", "Hết hiệu lực một phần"),
		doc("16750", "61/2005/QH11", "Luật Đấu thầu số 61/2005/QH11", "2005-11-29", "2006-04-01", "2014-07-01", "Hết hiệu lực toàn bộ"),
		doc("32842", "43/2013/QH13", "Luật Đấu thầu số 43/2013/QH13", "2013-11-26", "2014-07-01", "2024-01-01", "Hết hiệu lực toàn bộ"),
		doc("166581", "22/2023/QH15", "Luật Đấu thầu số 22/2023/QH15", "2023-06-23", "2024-01-01", "", "Hết hiệu lực một phần"),
		doc("173404", "57/2024/QH15", "Luật Sửa đổi Luật Quy hoạch, Luật Đầu tư, Luật Đầu tư theo phương thức đối tác công tư và Luật Đấu thầu 2024 số 57/2024/QH15", "2024-11-29", "2025-01-15", "", ""),
		doc("gd", "43/2019/QH14", "Luật Giáo dục số 43/2019/QH14", "2019-06-14", "2020-07-01", "", "Còn hiệu lực"),
		doc("amender", "99/2026/QH15", "Luật sửa đổi Luật Đấu thầu số 99/2026/QH15", "2026-03-01", "2026-05-01", "", "Còn hiệu lực"),
		// review blocker 1: a longer name must not fall back to a shorter law
		doc("dt14", "67/2014/QH13", "Luật Đầu tư số 67/2014/QH13", "2014-11-26", "2015-07-01", "2021-01-01", "Hết hiệu lực toàn bộ"),
		doc("dt20", "61/2020/QH14", "Luật Đầu tư số 61/2020/QH14", "2020-06-17", "2021-01-01", "", "Còn hiệu lực"),
		doc("ppp", "64/2020/QH14", "Luật Đầu tư theo phương thức đối tác công tư 2020 số 64/2020/QH14", "2020-06-18", "2021-01-01", "", "Còn hiệu lực"),
		doc("qhdt", "30/2009/QH12", "Luật Quy hoạch đô thị số 30/2009/QH12", "2009-06-17", "2010-01-01", "", "Còn hiệu lực"),
		// review blocker 2: newer same-name rows with missing dates
		doc("bv-old", "10/2010/QH12", "Luật Bảo vệ thử nghiệm số 10/2010/QH12", "2010-01-01", "2010-07-01", "", "Còn hiệu lực"),
		doc("bv-new", "20/2025/QH15", "Luật Bảo vệ thử nghiệm số 20/2025/QH15", "", "", "", "Còn hiệu lực"),
		doc("kt-old", "11/2011/QH12", "Luật Kiểm thử số 11/2011/QH12", "2011-01-01", "2011-07-01", "", "Còn hiệu lực"),
		doc("kt-new", "21/2025/QH15", "Luật Kiểm thử số 21/2025/QH15", "2025-06-01", "", "", "Còn hiệu lực"),
		// review nit: relation targets without a number key are told apart by id
		doc("pl-a", "", "Pháp lệnh Thử nghiệm", "2001-01-01", "", "", ""),
		doc("pl-b", "", "Pháp lệnh Thử nghiệm", "2003-01-01", "", "", ""),
		doc("amender2", "98/2026/QH15", "Luật sửa đổi Pháp lệnh Thử nghiệm số 98/2026/QH15", "2026-03-01", "2026-05-01", "", ""),
	}))
	require.NoError(t, catalog.ReplaceRelations(context.Background(), []*types.LegalDocumentRelation{
		{FromID: "amender", ToID: "32842", Relation: types.LegalRelationAmends},
		{FromID: "amender2", ToID: "pl-a", Relation: types.LegalRelationAmends},
		{FromID: "amender2", ToID: "pl-b", Relation: types.LegalRelationAmends},
	}))
	return db
}

func TestResolveLegalDocumentName(t *testing.T) {
	db := legalNameTestDB(t)
	ctx := context.Background()
	on := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	cases := []struct {
		name, at string
		want     LegalNameResolution
		chosen   string
		states   map[string]string
	}{
		// before the 2025 law: only 24/2018
		{"Luật An ninh mạng", "2019-05-01", LegalNameUnique, "24/2018/QH14", nil},
		// between the 2025 law's issue and effective date: either
		{"Luật An ninh mạng", "2026-01-15", LegalNameAmbiguous, "",
			map[string]string{"116/2025/QH15": LegalNamePending, "24/2018/QH14": LegalNameInForce}},
		// after 01/07/2026 the 2025 law supersedes 24/2018 though its status
		// still says "Còn hiệu lực"
		{"Luật An ninh mạng", "2026-08-01", LegalNameUnique, "116/2025/QH15",
			map[string]string{"116/2025/QH15": LegalNameInForce, "24/2018/QH14": LegalNameSuperseded}},
		{"Luật Đấu thầu", "2010-01-01", LegalNameUnique, "61/2005/QH11", nil},
		{"Luật Đấu thầu", "2015-03-01", LegalNameUnique, "43/2013/QH13",
			map[string]string{"61/2005/QH11": LegalNameExpired, "43/2013/QH13": LegalNameInForce}},
		{"Luật Đấu thầu", "2023-08-01", LegalNameAmbiguous, "",
			map[string]string{"43/2013/QH13": LegalNameInForce, "22/2023/QH15": LegalNamePending}},
		{"Luật Đấu thầu", "2024-05-01", LegalNameUnique, "22/2023/QH15",
			map[string]string{"43/2013/QH13": LegalNameExpired}},
		// the amending law of 2024 is not "Luật Đấu thầu"
		{"Luật Đấu thầu", "2025-02-01", LegalNameUnique, "22/2023/QH15", nil},
		{"Luật Đấu thầu", "2005-01-01", LegalNameNone, "", nil},
		// a reader that ran past the name
		{"Luật Đấu thầu, theo đó", "2024-05-01", LegalNameUnique, "22/2023/QH15", nil},
		// a longer name never resolves to a shorter law: unsure, so ambiguous
		{"Luật Giáo dục đại học", "2024-05-01", LegalNameAmbiguous, "", nil},
		{"luật giáo dục", "2024-05-01", LegalNameUnique, "43/2019/QH14", nil},
		{"Luật", "2024-05-01", LegalNameNone, "", nil},
		// review blocker 1
		{"Luật Đầu tư theo phương thức đối tác công tư", "2022-01-01", LegalNameUnique, "64/2020/QH14", nil},
		{"Luật Đầu tư theo phương thức đối tác công tư", "2020-03-01", LegalNameNone, "", nil},
		{"Luật Quy hoạch đô thị và nông thôn", "2025-03-01", LegalNameAmbiguous, "", nil},
		{"Luật Đầu tư có hiệu lực từ", "2022-01-01", LegalNameUnique, "61/2020/QH14", nil},
		// review blocker 2
		{"Luật Bảo vệ thử nghiệm", "2026-01-01", LegalNameAmbiguous, "",
			map[string]string{"20/2025/QH15": LegalNameUndated, "10/2010/QH12": LegalNameInForce}},
		{"Luật Kiểm thử", "2026-01-01", LegalNameAmbiguous, "",
			map[string]string{"21/2025/QH15": LegalNamePending, "11/2011/QH12": LegalNameInForce}},
		// review should 4: a year in the name narrows by issue year
		{"Luật Đấu thầu 2013", "2023-08-01", LegalNameUnique, "43/2013/QH13", nil},
		{"Luật Đấu thầu năm 2023", "2023-08-01", LegalNameAmbiguous, "", nil},
	}
	for _, c := range cases {
		m, err := ResolveLegalDocumentName(ctx, db, LegalNameQuery{Name: c.name, At: on(c.at)})
		require.NoError(t, err)
		require.Equal(t, c.want, m.Resolution, "%s @ %s", c.name, c.at)
		if c.chosen != "" {
			require.NotNil(t, m.Chosen)
			require.Equal(t, c.chosen, m.Chosen.DocumentNumber, "%s @ %s", c.name, c.at)
		} else {
			require.Nil(t, m.Chosen, "%s @ %s", c.name, c.at)
		}
		got := map[string]string{}
		for _, cand := range m.Candidates {
			got[cand.Document.DocumentNumber] = cand.State
		}
		for num, st := range c.states {
			require.Equal(t, st, got[num], "%s @ %s: state of %s", c.name, c.at, num)
		}
	}

	// the amending law is recorded to amend 43/2013: that wins over dates
	m, err := ResolveLegalDocumentName(ctx, db, LegalNameQuery{Name: "Luật Đấu thầu", At: on("2026-03-01"), AmenderID: "amender"})
	require.NoError(t, err)
	require.Equal(t, LegalNameUnique, m.Resolution)
	require.Equal(t, "43/2013/QH13", m.Chosen.DocumentNumber)

	// two recorded targets without a number key are two documents
	m, err = ResolveLegalDocumentName(ctx, db, LegalNameQuery{Name: "Pháp lệnh Thử nghiệm", At: on("2026-03-01"), AmenderID: "amender2"})
	require.NoError(t, err)
	require.Equal(t, LegalNameAmbiguous, m.Resolution)
	require.Len(t, m.Candidates, 2)

	// the Year field narrows like a year in the name
	m, err = ResolveLegalDocumentName(ctx, db, LegalNameQuery{Name: "Luật Đấu thầu", Year: "2023", At: on("2024-05-01")})
	require.NoError(t, err)
	require.Equal(t, LegalNameUnique, m.Resolution)
	require.Equal(t, "22/2023/QH15", m.Chosen.DocumentNumber)
}

func TestLegalHistoryLookupAmendedByDate(t *testing.T) {
	db := legalNameTestDB(t)
	h := &legalHistory{amended: map[string]string{}, ctx: context.Background(), db: db}
	if got := h.amendedNumber(legalHistoryDoc{Number: "1/2026/QH15", IssueDate: "2026-08-01"}, "Luật An ninh mạng"); got != "116/2025/QH15" {
		t.Errorf("after 01/07/2026 = %q", got)
	}
	if got := h.amendedNumber(legalHistoryDoc{Number: "2/2026/QH15", IssueDate: "2026-01-15"}, "Luật An ninh mạng"); got != "" {
		t.Errorf("ambiguous window = %q, want none", got)
	}
	if got := h.amendedNumber(legalHistoryDoc{Number: "x", ExternalID: "amender", IssueDate: "2026-03-01"}, "Luật Đấu thầu"); got != "43/2013/QH13" {
		t.Errorf("recorded relation = %q", got)
	}
	if got := h.amendedNumber(legalHistoryDoc{Number: "z", IssueDate: "01/08/2026"}, "Luật An ninh mạng"); got != "" {
		t.Errorf("unparsable issue date = %q, want none", got)
	}
	if got := h.amendedNumber(legalHistoryDoc{Number: "y"}, "Nghị định số 75/2019/NĐ-CP"); got != "75/2019/NĐ-CP" {
		t.Errorf("written number = %q", got)
	}
}

func TestLegalTitleBaseWithType(t *testing.T) {
	for _, c := range [][3]string{
		{"An ninh mạng", "Luật", "luật an ninh mạng"},
		{"LUẬT AN NINH MẠNG SỐ 116/2025/QH15", "Luật", "luật an ninh mạng"},
		{"Sửa đổi, bổ sung một số điều của Luật Đấu thầu", "Luật", "luật sửa đổi, bổ sung một số điều của luật đấu thầu"},
		{"Bộ luật Lao động số 45/2019/QH14", "Bộ luật", "bộ luật lao động"},
		{"Luật Đầu tư theo phương thức đối tác công tư 2020 số 64/2020/QH14", "Luật", "luật đầu tư theo phương thức đối tác công tư"},
		{"Luật Đấu thầu năm 2013", "", "luật đấu thầu"},
	} {
		if got := LegalTitleBase(c[0], c[1]); got != c[2] {
			t.Errorf("LegalTitleBase(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
