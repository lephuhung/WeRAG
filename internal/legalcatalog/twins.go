package legalcatalog

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// TwinResolution is what ResolveTwins makes of an import.
type TwinResolution struct {
	// Keep is the import without the new ids of catalogued documents, in
	// order.
	Keep []*types.LegalDocument
	// Alias maps a twin's id to its surviving row: edges at the twin
	// belong to the survivor.
	Alias map[string]string
	// Unreachable lists the catalogued Ð rows left with their Ð key and
	// attached to no row (migration 000175's twins that are not the same
	// document as a row of the folded key), for a SuperAdmin decision.
	Unreachable []Twin
	// StatusDiffers lists the attached twins whose status is not their
	// survivor's: the key answers the survivor's, worth a SuperAdmin look.
	StatusDiffers []Attached
}

// Attached is a twin and the row it is attached to.
type Attached struct {
	Twin, Survivor *types.LegalDocument
}

// Twin is a catalogued row kept out of the number lookup and the rows
// holding its folded key.
type Twin struct {
	Row     *types.LegalDocument
	Holders []string
}

// ResolveTwins matches an import against the catalogue so that one
// document written two ways ("02/2000/QÐ-BTC" with the Eth look-alike or
// "51/2025/TT-BTС" with a Cyrillic С, and the same number in Latin
// letters, under two vbpl ids) never becomes two rows under one key.
// existing holds the catalogue rows (id, document_number, number_key,
// doc_type, title, issuer, issued_on, status, reviewed_at are read); docs
// are the import's rows, numbers already cleaned (CleanVBPLNumber) and
// keyed (CatalogNumberKey). A row is matched by id first, otherwise as
// the same document (SameDocument).
//
//   - A new id that is the same document as a catalogued or earlier
//     imported row is not imported: Alias maps it to that row. When that
//     row is a placeholder ("(tạm nhập - chờ toàn văn)") and the new one
//     is not, the full copy is imported instead and the placeholder is
//     aliased to it: its edges move to the full copy.
//
//   - A catalogued row keyed with a look-alike whose folded key other rows
//     hold, and that is the same document as one of them, keeps its number
//     and key (the rest is updated) and is aliased to it: the key keeps
//     answering one row with one status. The surviving row is the holder,
//     already the answer to the key, unless the look-alike row was
//     reviewed and the holder was not, or the holder is a placeholder:
//     then the look-alike row is rekeyed (both stay under the key) and,
//     for a placeholder, the placeholder is aliased to it.
//
//   - Not the same document: a row keyed with Ð keeps its key (the twins
//     migration 000175 left for a SuperAdmin, listed in Unreachable); any
//     other look-alike (Cyrillic, dashes) is rekeyed.
//
//   - Of several rows that are the same document, a full copy is matched
//     before a placeholder, and the import is weighed in a fixed order
//     (catalogued rows, then new ones, each by id): the surviving row and
//     the placeholder's alias do not depend on the file's order, and two
//     new full copies of one placeholder are one row.
//
// An attached twin keeps its row: KB documents point at it by id
// (knowledge metadata external_id, legal_document_links), so it stays
// readable by id; only the number lookup and the edges go to the survivor.
func ResolveTwins(existing, docs []*types.LegalDocument) TwinResolution {
	byID := make(map[string]*types.LegalDocument, len(existing))
	incoming := make(map[string]*types.LegalDocument, len(docs))
	for _, d := range docs {
		incoming[d.ID] = d
	}
	byKey := map[string][]*types.LegalDocument{}
	for _, e := range existing {
		byID[e.ID] = e
		// a row still keyed with a look-alike is not a holder of the
		// folded key
		if e.NumberKey == "" || vietnamese_legal.CatalogNumberKey(e.NumberKey) != e.NumberKey {
			continue
		}
		// a row the import carries holds its key as imported (a
		// placeholder filled on the portal is a full copy now); under
		// another key it joins that one in the loop
		if d := incoming[e.ID]; d != nil {
			if d.NumberKey == e.NumberKey {
				byKey[e.NumberKey] = append(byKey[e.NumberKey], d)
			}
			continue
		}
		byKey[e.NumberKey] = append(byKey[e.NumberKey], e)
	}
	res := TwinResolution{Alias: map[string]string{}, Keep: docs[:0:0]}
	attach := func(twin, survivor *types.LegalDocument) {
		res.Alias[twin.ID] = survivor.ID
		if strings.TrimSpace(twin.Status) != strings.TrimSpace(survivor.Status) {
			res.StatusDiffers = append(res.StatusDiffers, Attached{Twin: twin, Survivor: survivor})
		}
	}
	// replace aliases a placeholder to its full copy once: a later full
	// copy that matches the placeholder but not that copy is another row.
	// Every other placeholder of the key that is the same document goes
	// to that copy too: one document entered twice before its text came.
	replace := func(placeholder, full *types.LegalDocument) {
		if _, done := res.Alias[placeholder.ID]; !done {
			attach(placeholder, full)
		}
		for _, p := range byKey[placeholder.NumberKey] {
			if _, done := res.Alias[p.ID]; !done && p.ID != full.ID && IsPlaceholder(p.Title) && SameDocument(p, full) {
				attach(p, full)
			}
		}
	}
	dropped := map[*types.LegalDocument]bool{}
	for _, d := range resolveOrder(docs, byID) {
		cur := byID[d.ID]
		if cur == nil {
			if s := sameDocumentIn(byKey[d.NumberKey], d); s != nil {
				if !IsPlaceholder(s.Title) || IsPlaceholder(d.Title) {
					attach(d, s)
					dropped[d] = true
					continue
				}
				replace(s, d) // the full copy replaces the placeholder
			}
			// a catalogued Ð twin left unreachable (weighed first) is
			// attached to its new Latin copy
			unreachable := res.Unreachable[:0]
			for _, u := range res.Unreachable {
				if _, done := res.Alias[u.Row.ID]; !done && vietnamese_legal.CatalogNumberKey(u.Row.NumberKey) == d.NumberKey &&
					SameDocument(u.Row, d) {
					attach(u.Row, d)
					continue
				}
				unreachable = append(unreachable, u)
			}
			res.Unreachable = unreachable
		} else if cur.NumberKey != d.NumberKey && vietnamese_legal.CatalogNumberKey(cur.NumberKey) == d.NumberKey {
			if holders := others(byKey[d.NumberKey], d.ID); len(holders) > 0 {
				s := sameDocumentIn(holders, d)
				placeholder := s != nil && IsPlaceholder(s.Title) && !IsPlaceholder(d.Title)
				switch {
				case s != nil && (cur.ReviewedAt == nil || s.ReviewedAt != nil) && !placeholder:
					attach(d, s)
					d.DocumentNumber, d.NumberKey = cur.DocumentNumber, cur.NumberKey
					continue
				case placeholder:
					replace(s, d) // rekeyed below, the placeholder's edges with it
				case s == nil && strings.ContainsAny(cur.NumberKey, "Ðð"):
					var ids []string
					for _, h := range holders {
						ids = append(ids, h.ID)
					}
					res.Unreachable = append(res.Unreachable, Twin{Row: d, Holders: ids})
					d.DocumentNumber, d.NumberKey = cur.DocumentNumber, cur.NumberKey
					continue
				}
				// else: a reviewed copy is not hidden behind an unreviewed
				// one, it is rekeyed
			}
		}
		if d.NumberKey != "" && (cur == nil || cur.NumberKey != d.NumberKey) {
			byKey[d.NumberKey] = append(byKey[d.NumberKey], d)
		}
	}
	for _, d := range docs {
		if !dropped[d] {
			res.Keep = append(res.Keep, d)
		}
	}
	return res
}

// resolveOrder is the order ResolveTwins weighs an import in, whatever
// the file's: catalogued rows first (a row already in the catalogue is
// kept over a new copy of it), then new rows, each by id. So of one
// document under several ids the same row survives every time.
func resolveOrder(docs []*types.LegalDocument, byID map[string]*types.LegalDocument) []*types.LegalDocument {
	out := append([]*types.LegalDocument(nil), docs...)
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := byID[out[i].ID] != nil, byID[out[j].ID] != nil
		if ci != cj {
			return ci
		}
		return idLess(out[i].ID, out[j].ID)
	})
	return out
}

// idLess orders vbpl ids numerically ("9" before "10"), other ids as text.
func idLess(a, b string) bool {
	digits := func(s string) bool {
		return s != "" && strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
	}
	if digits(a) && digits(b) && len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// sameDocumentIn returns the row of rows that is d under another id,
// preferring a full copy to a placeholder ("(tạm nhập - chờ toàn văn)")
// when several are: the placeholder is replaced by that copy, not by d.
func sameDocumentIn(rows []*types.LegalDocument, d *types.LegalDocument) *types.LegalDocument {
	var placeholder *types.LegalDocument
	for _, r := range rows {
		if r.ID == d.ID || !SameDocument(r, d) {
			continue
		}
		if !IsPlaceholder(r.Title) {
			return r
		}
		if placeholder == nil {
			placeholder = r
		}
	}
	return placeholder
}

// SameDocument reports whether two catalogue rows of one number key are
// one document: the same type class and known issue day, issuers that do
// not contradict (IssuersAgree), the same places named in both titles
// (PlacesAgree) and titles that agree once the type and number they open
// with are dropped (TitleMatch). Two documents sharing a number (an old
// number reused, "05/2020/QĐ-UBND" of two provinces) differ in day,
// issuer, place or subject; English copies of the portal differ in title.
func SameDocument(a, b *types.LegalDocument) bool {
	return a.IssuedOn != nil && b.IssuedOn != nil &&
		a.IssuedOn.Format("2006-01-02") == b.IssuedOn.Format("2006-01-02") &&
		vietnamese_legal.DocTypeClass(a.DocType) == vietnamese_legal.DocTypeClass(b.DocType) &&
		IssuersAgree(a.Issuer, b.Issuer) && PlacesAgree(a.Title, b.Title) &&
		TitleMatch(a.Title, a.DocumentNumber, b.Title, b.DocumentNumber)
}

// placeholderRe is the portal's mark of a row without its text yet:
// "(tạm nhập - chờ toàn văn)", "(Chờ toàn văn)", "(tạm nhập, chờ toàn văn)".
var placeholderRe = regexp.MustCompile(`(?i)\(\s*(?:tạm\s+nhập\s*[-–,]?\s*)?chờ\s+toàn\s+văn\s*\)`)

// IsPlaceholder reports whether a catalogue title marks a row waiting for
// its full text.
func IsPlaceholder(title string) bool { return placeholderRe.MatchString(title) }

// IssuersAgree reports whether two issuer fields can name the same
// issuer: the places of local issuers agree ("Ủy ban nhân dân tỉnh Hà
// Giang" and "UBND tỉnh Hà Nam" do not). Central issuers are not compared:
// the portal writes one decree's issuer as "Chính phủ" or the drafting
// ministry, a renamed ministry under either name, a joint circular's as
// one ministry or all of them in any order — the same document every time.
func IssuersAgree(a, b string) bool {
	return PlacesAgree(issuerAbbrev.Replace(a), issuerAbbrev.Replace(b))
}

// issuerAbbrev spells out "UBND" / "HĐND" / "TP." so their place is read.
var issuerAbbrev = strings.NewReplacer("UBND ", "Ủy ban nhân dân ", "HĐND ", "Hội đồng nhân dân ", "TP. ", "thành phố ", "TP.", "thành phố ")

// placeRe is a place a title names: "tỉnh Hà Giang", "thành phố Hồ Chí
// Minh", "huyện Mường Khương", "xã Tân Lập" (title-cased words after the
// unit).
var placeRe = regexp.MustCompile(`(?:^|[^\p{L}])((?i:tỉnh|thành\s+phố|huyện|quận|thị\s+xã|thị\s+trấn|xã|phường))\s+((?:\p{Lu}\p{Ll}*(?:\s+|$))+)`)

// PlacesAgree reports whether two titles that both name places share
// one (one place's words a prefix of the other's: "Thừa Thiên Huế" and
// "Thừa Thiên"): "… tỉnh Hà Giang" and "… tỉnh Hà Nam" are two
// provinces' decisions. A reading one title has and the other writes as
// common words ("chính sách Xã Hội" and "chính sách xã hội", "Đề án Xã
// Nông thôn mới" and "xã nông thôn mới") is title case, not a place.
func PlacesAgree(a, b string) bool {
	pa, pb := titlePlaces(a), titlePlaces(b)
	pa, pb = withoutCommonWords(pa, pb, b), withoutCommonWords(pb, pa, a)
	if len(pa) == 0 || len(pb) == 0 {
		return true
	}
	for _, x := range pa {
		for _, y := range pb {
			if prefixAgree(x.name, y.name) {
				return true
			}
		}
	}
	return false
}

func prefixAgree(x, y []string) bool {
	if len(x) > len(y) {
		x, y = y, x
	}
	return strings.Join(y[:len(x)], " ") == strings.Join(x, " ")
}

// withoutCommonWords drops the places of one title that the other title
// (other, read as places in theirs) writes in its words without reading
// them as a place: the same words, only cased otherwise.
func withoutCommonWords(places, theirs []titlePlace, other string) []titlePlace {
	text := " " + strings.Join(lowerWords(other), " ") + " "
	var out []titlePlace
	for _, x := range places {
		read := false
		for _, y := range theirs {
			if y.unit == x.unit && prefixAgree(x.name, y.name) {
				read = true
				break
			}
		}
		if !read && strings.Contains(text, " "+x.unit+" "+strings.Join(x.name, " ")+" ") {
			continue
		}
		out = append(out, x)
	}
	return out
}

// lowerWords is a title's letter-and-digit words, look-alikes folded,
// lower case.
func lowerWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(vietnamese_legal.FoldNumberLookalikes(s)),
		func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// placeStop are title-cased words that start the rest of a title after a
// place ("tỉnh Hà Giang Về việc …"), not part of its name.
var placeStop = map[string]bool{"về": true, "ban": true, "quy": true, "sửa": true, "bổ": true, "hướng": true,
	"phê": true, "thực": true, "ngày": true, "năm": true, "giai": true, "đến": true, "theo": true, "của": true,
	"và": true, "trong": true, "tại": true, "thuộc": true, "giao": true, "điều": true, "bãi": true, "thành": true}

// titlePlace is a place read in a title: its unit ("tỉnh", "thành phố")
// and name words, lower case.
type titlePlace struct {
	unit string
	name []string
}

func titlePlaces(title string) []titlePlace {
	var out []titlePlace
	title = vietnamese_legal.FoldNumberLookalikes(title)
	// the next place may start right after this one's name ("chính sách
	// Xã Hội tỉnh Hà Giang"): the search goes on before the space the
	// name's last word consumed
	for pos := 0; pos < len(title); {
		loc := placeRe.FindStringSubmatchIndex(title[pos:])
		if loc == nil {
			break
		}
		unit, run := title[pos+loc[2]:pos+loc[3]], title[pos+loc[4]:pos+loc[5]]
		pos += loc[4] + len(strings.TrimRightFunc(run, unicode.IsSpace))
		var name []string
		for _, w := range strings.Fields(strings.ToLower(run)) {
			if placeStop[w] || len(name) == 4 {
				break
			}
			name = append(name, w)
		}
		if len(name) > 0 {
			out = append(out, titlePlace{unit: strings.Join(lowerWords(unit), " "), name: name})
		}
	}
	return out
}

// Thresholds of TitleMatch, set on the dev catalogue (2026-11-02): of the
// 292 Ð twin pairs and 11 Cyrillic pairs of one key, type and day, they
// accept 284 and 11; the 8 refused are English copies, a different
// subject (41/2000/QĐ-BTC) and an abbreviated title ("NSNN"). A
// "<type> số <number>" prefix, "của Bộ trưởng …", "V/v" for "về việc" and
// OCR typos ("bố sung", "hệ thóng") pass.
const (
	titleMinRatio      = 0.85 // 1 - edit distance / longer length
	titleMinContain    = 0.85 // words of the shorter title found in the longer
	titleMinContainLen = 3    // words the shorter needs for containment
)

// TitleMatch reports whether two catalogue titles name one document:
// after the opening type and number, the letters agree up to a small edit
// distance, or nearly all words of the shorter appear in the longer.
func TitleMatch(titleA, numberA, titleB, numberB string) bool {
	a := titleWords(placeholderRe.ReplaceAllString(titleA, " "), numberA)
	b := titleWords(placeholderRe.ReplaceAllString(titleB, " "), numberB)
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b) // both no more than "<type> số <number>"
	}
	sa, sb := []rune(strings.Join(a, "")), []rune(strings.Join(b, ""))
	if 1-float64(editDistance(sa, sb))/float64(max(len(sa), len(sb))) >= titleMinRatio {
		return true
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(a) < titleMinContainLen {
		return false
	}
	left := map[string]int{}
	for _, w := range b {
		left[w]++
	}
	hit := 0
	for _, w := range a {
		if left[w] > 0 {
			left[w]--
			hit++
		}
	}
	return float64(hit)/float64(len(a)) >= titleMinContain
}

// titleWords is a title's words after its opening type and number:
// look-alikes folded, lower case, "v/v" read as "về việc".
func titleWords(title, number string) []string {
	words := func(s string) []string {
		s = strings.ReplaceAll(strings.ToLower(vietnamese_legal.FoldNumberLookalikes(s)), "v/v", "về việc")
		return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	t, n := words(title), words(number)
	// the number within the first words ("Thông tư liên tịch số 22/2003/…")
	for i := 0; len(n) > 0 && i+len(n) <= len(t) && i < 8; i++ {
		if strings.Join(t[i:i+len(n)], " ") == strings.Join(n, " ") {
			return t[i+len(n):]
		}
	}
	return t
}

func editDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func others(rows []*types.LegalDocument, id string) []*types.LegalDocument {
	var out []*types.LegalDocument
	for _, r := range rows {
		if r.ID != id {
			out = append(out, r)
		}
	}
	return out
}
