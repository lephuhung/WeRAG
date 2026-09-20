package people

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// PersonRecord is one sanitized MongoDB document tagged with its source
// collection and the identity group it was merged into (1-based).
type PersonRecord map[string]any

// canonLabels mirrors _CANON_LABELS: canonical attribute → display label,
// in render order.
var canonLabels = []struct{ key, label string }{
	{"name", "Họ tên"},
	{"dob", "Ngày sinh"},
	{"cccd", "CCCD/CMND"},
	{"phone", "Điện thoại"},
	{"bhxh", "Mã số BHXH"},
	{"address", "Địa chỉ"},
}

// fieldLabels mirrors the label_map in AIRAG _build_display_text.
var fieldLabels = map[string]string{
	// BHXH fields
	"hoTen": "Họ tên", "maSoBhxh": "Mã số BHXH", "soTheBhyt": "Số thẻ BHYT",
	"ngaySinhHienThi": "Ngày sinh", "trangThaiThe": "Trạng thái thẻ", "tyLeBhyt": "Tỷ lệ BHYT",
	"tuNgay": "Từ ngày", "denNgay": "Đến ngày", "coSoKCB": "Cơ sở KCB",
	"soDienThoai": "Điện thoại", "soCmnd": "Số CMND/CCCD", "diaChi": "Địa chỉ",
	// LG fields
	"TenHoiVien": "Họ tên", "SoDienThoai": "Điện thoại", "DiaChi": "Địa chỉ",
	"DiemHoiVien": "Điểm hội viên", "TenHangHoiVien": "Hạng hội viên",
	"SoTheHoiVien": "Số thẻ hội viên", "SoDinhDanh": "Số CCCD/CMND", "NgaySinh": "Ngày sinh",
	// Vacxin fields
	"HO_TEN": "Họ tên", "NGAY_SINH": "Ngày sinh", "TEN_ME": "Tên mẹ",
	"DIEN_THOAI_ME": "Điện thoại mẹ", "MA_DOI_TUONG": "Mã định danh",
	"GIOI_TINH": "Giới tính", "PID": "Mã PID",
	// VNVC fields
	"mobile": "Điện thoại", "fullName": "Họ tên", "fullNam": "Ngày sinh",
	// CV19 fields
	"so_dien_thoai": "Điện thoại", "ho_ten": "Họ tên", "namsinh": "Năm sinh",
	"gioi_tinh": "Giới tính", "dia_chi": "Địa chỉ",
	// EVN fields
	"tenKhachHang": "Tên khách hàng", "cmnd": "Số CMND", "phone": "Điện thoại",
	"diaChiCapDien": "Địa chỉ cấp điện", "ngayDangKy": "Ngày đăng ký",
	// UIDS fields
	"uid": "Facebook UID",
}

// objectIDExact matches a whole-string MongoDB ObjectId (exactly 24 hex chars).
var objectIDExact = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// bracketedObjectID matches "[<24 hex>]" occurrences inside display text.
var bracketedObjectID = regexp.MustCompile(`\[[0-9a-fA-F]{24}\]`)

// hexRun matches runs of hex chars for the bare-ObjectId strip below.
var hexRun = regexp.MustCompile(`[0-9a-fA-F]+`)

// stripObjectIDText removes embedded ObjectIds from rendered text, porting
// AIRAG _STRIP_OBJECT_ID_RE: "[hex24]" plus standalone hex runs of exactly
// 24 chars. Go regexp has no lookaround, so the "exactly 24" rule is applied
// by checking the length of each hex run rather than negated classes.
func stripObjectIDText(s string) string {
	s = bracketedObjectID.ReplaceAllString(s, "")
	return hexRun.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) == 24 {
			return ""
		}
		return m
	})
}

// digitsOnly strips non-digit characters.
var nonDigit = regexp.MustCompile(`\D`)

// isObjectID reports whether a value is (or contains) a MongoDB ObjectId —
// either the decoded bson.ObjectID type or a bare 24-hex string, mirroring
// AIRAG _is_object_id.
func isObjectID(v any) bool {
	switch t := v.(type) {
	case bson.ObjectID:
		return true
	case string:
		return objectIDExact.MatchString(t)
	case bson.A:
		for _, e := range t {
			if isObjectID(e) {
				return true
			}
		}
	case bson.M:
		for _, e := range t {
			if isObjectID(e) {
				return true
			}
		}
	case bson.D:
		for _, e := range t {
			if isObjectID(e.Value) {
				return true
			}
		}
	}
	return false
}

// sanitizeRecord returns a copy of doc with every ObjectId-bearing field
// dropped, mirroring _sanitize_record.
func sanitizeRecord(doc bson.M) bson.M {
	out := bson.M{}
	for k, v := range doc {
		if isObjectID(v) {
			continue
		}
		switch t := v.(type) {
		case bson.A:
			kept := make(bson.A, 0, len(t))
			for _, e := range t {
				if !isObjectID(e) {
					kept = append(kept, e)
				}
			}
			out[k] = kept
		case bson.M:
			clean := bson.M{}
			for k2, e := range t {
				if !isObjectID(e) {
					clean[k2] = e
				}
			}
			out[k] = clean
		default:
			out[k] = v
		}
	}
	return out
}

// fieldsFor returns the query fields of schema under lookupType, mirroring
// _field_for (advanced map contributes its canonical field values).
func fieldsFor(schema, lookupType string) []string {
	cfg, ok := SearchableCollectionMap[lookupType]
	if !ok {
		return nil
	}
	sc, ok := cfg.Collections[schema]
	if !ok {
		return nil
	}
	if len(sc.CanonFields) > 0 {
		out := make([]string, 0, len(sc.CanonFields))
		for _, f := range sc.CanonFields {
			out = append(out, f)
		}
		return out
	}
	return sc.Fields
}

// extractCanonical pulls normalized attributes (name/cccd/phone/bhxh/dob/
// address) from one document, mirroring _extract_canonical.
func extractCanonical(schema string, doc bson.M) map[string]string {
	out := map[string]string{}
	for _, pair := range [][2]string{
		{"cccd", "cccd"}, {"bhxh", "bhxh"}, {"phone", "phone"}, {"name", "name"},
	} {
		canon, lt := pair[0], pair[1]
		for _, f := range fieldsFor(schema, lt) {
			if v, ok := doc[f]; ok && v != nil {
				s := strings.TrimSpace(fmt.Sprint(v))
				if s != "" && s != "None" {
					out[canon] = s
					break
				}
			}
		}
	}
	// dob/address (and missing name/phone) come from the advanced map.
	adv := SearchableCollectionMap["advanced"].Collections[schema].CanonFields
	for _, canon := range []string{"name", "dob", "address", "phone"} {
		if out[canon] != "" {
			continue
		}
		if f, ok := adv[canon]; ok && f != "" {
			if v, ok2 := doc[f]; ok2 && v != nil {
				s := strings.TrimSpace(fmt.Sprint(v))
				if s != "" && s != "None" {
					out[canon] = s
				}
			}
		}
	}
	return out
}

// identityKey groups records belonging to the same person — strong
// identifiers (CCCD, BHXH) first, then name+(dob|phone). Records are never
// merged on phone alone: several people can share one contact number.
func identityKey(canon map[string]string, docID string) string {
	if v := canon["cccd"]; v != "" {
		if d := nonDigit.ReplaceAllString(v, ""); d != "" {
			return "cccd:" + d
		}
	}
	if v := canon["bhxh"]; v != "" {
		if d := nonDigit.ReplaceAllString(v, ""); d != "" {
			return "bhxh:" + d
		}
	}
	name := strings.ToLower(strings.TrimSpace(canon["name"]))
	extra := canon["dob"]
	if extra == "" {
		extra = canon["phone"]
	}
	if name != "" {
		return "np:" + name + "|" + extra
	}
	return "id:" + docID
}

// docIDString renders the _id value as a string for identity grouping.
func docIDString(doc bson.M) string {
	switch t := doc["_id"].(type) {
	case bson.ObjectID:
		return t.Hex()
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// docToMap flattens a bson.D into a string→value map (v2 removed D.Map()).
func docToMap(doc bson.D) map[string]any {
	m := make(map[string]any, len(doc))
	for _, e := range doc {
		m[e.Key] = e.Value
	}
	return m
}

// buildDisplayText renders one document for the per-source detail section,
// mirroring _build_display_text.
func buildDisplayText(doc bson.D, displayFields []string, schema string) string {
	if len(doc) == 0 {
		return ""
	}
	m := docToMap(doc)
	fieldsToShow := displayFields
	if len(fieldsToShow) < 3 {
		fieldsToShow = fieldsToShow[:0]
		for _, e := range doc {
			k := e.Key
			if k == "_id" || k == "id" || strings.HasPrefix(k, "_") || isObjectID(e.Value) {
				continue
			}
			fieldsToShow = append(fieldsToShow, k)
		}
	}

	var parts []string
	shownAny := false
	for _, field := range fieldsToShow {
		val, ok := m[field]
		if !ok || isObjectID(val) {
			continue
		}
		label := field
		if l, ok := fieldLabels[field]; ok {
			label = l
		}
		var s string
		if field == "GIOI_TINH" || field == "gioi_tinh" {
			switch fmt.Sprint(val) {
			case "1":
				s = "Nam"
			case "0":
				s = "Nữ"
			default:
				s = fmt.Sprint(val)
			}
		} else {
			s = fmt.Sprint(val)
		}
		if s == "" || s == "<nil>" || s == "None" {
			s = "—"
		} else {
			shownAny = true
		}
		parts = append(parts, "      - "+label+": "+s)
	}
	if !shownAny && len(fieldsToShow) > 0 {
		parts = append(parts, "      (không có thêm thông tin chi tiết trong hồ sơ)")
	}
	return stripObjectIDText(strings.Join(parts, "\n"))
}

// buildProfileBlock renders one consolidated person block, mirroring
// _build_profile_block.
func buildProfileBlock(
	idx int, merged map[string][]string, members []groupedDoc, lookupType string,
) string {
	var lines []string
	lines = append(lines, strings.Repeat("═", 34), fmt.Sprintf("👤 HỒ SƠ #%d", idx), strings.Repeat("═", 34))
	for _, cl := range canonLabels {
		if vals := merged[cl.key]; len(vals) > 0 {
			lines = append(lines, "  • "+cl.label+": "+strings.Join(vals, ", "))
		}
	}

	// Source list with per-source record counts, first-seen order.
	counts := map[string]int{}
	var order []string
	for _, m := range members {
		if _, seen := counts[m.schema]; !seen {
			order = append(order, m.schema)
		}
		counts[m.schema]++
	}
	var srcParts []string
	for _, s := range order {
		name := SchemaDisplayName(s)
		if counts[s] > 1 {
			srcParts = append(srcParts, fmt.Sprintf("%s (%d)", name, counts[s]))
		} else {
			srcParts = append(srcParts, name)
		}
	}
	lines = append(lines, "  • Nguồn dữ liệu: "+strings.Join(srcParts, ", "))
	lines = append(lines, "  ── Chi tiết theo nguồn ──")

	for _, m := range members {
		cfg := SearchableCollectionMap[lookupType].Collections[m.schema]
		detail := buildDisplayText(m.docD, cfg.DisplayFields, m.schema)
		lines = append(lines, "  ▸ "+SchemaDisplayName(m.schema))
		if strings.TrimSpace(detail) != "" {
			lines = append(lines, detail)
		}
	}
	return strings.Join(lines, "\n")
}

type groupedDoc struct {
	doc    bson.M // sanitized-ish raw doc (still carries _source bookkeeping)
	docD   bson.D // original ordered doc for display
	canon  map[string]string
	schema string
}

// consolidate merges per-schema results into per-person profiles, mirroring
// _consolidate. Returns persons (flat sanitized records with
// _source_schema/_person_group), the rendered display text, and the sorted
// list of collections that produced hits.
func consolidate(resultsBySchema map[string][]bson.D, lookupType string) ([]PersonRecord, string, []string) {
	var flat []groupedDoc
	for schema, docs := range resultsBySchema {
		for _, d := range docs {
			flat = append(flat, groupedDoc{doc: docToMap(d), docD: d, schema: schema})
		}
	}
	if len(flat) == 0 {
		return nil, "", nil
	}

	groups := map[string][]groupedDoc{}
	var order []string
	for _, gd := range flat {
		gd.canon = extractCanonical(gd.schema, gd.doc)
		key := identityKey(gd.canon, docIDString(gd.doc))
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], gd)
	}

	var persons []PersonRecord
	var blocks []string
	schemaSet := map[string]bool{}

	for idx, key := range order {
		members := groups[key]
		merged := map[string][]string{}
		for _, m := range members {
			schemaSet[m.schema] = true
			for k, v := range m.canon {
				if v == "" {
					continue
				}
				seen := false
				for _, e := range merged[k] {
					if e == v {
						seen = true
						break
					}
				}
				if !seen {
					merged[k] = append(merged[k], v)
				}
			}
			rec := PersonRecord(sanitizeRecord(m.doc))
			rec["_source_schema"] = m.schema
			rec["_person_group"] = idx + 1
			persons = append(persons, rec)
		}
		blocks = append(blocks, buildProfileBlock(idx+1, merged, members, lookupType))
	}

	var header string
	if len(order) == 1 {
		header = fmt.Sprintf("✅ Tìm thấy **1 người** (%d hồ sơ từ các nguồn dữ liệu):\n", len(flat))
	} else {
		header = fmt.Sprintf("✅ Tìm thấy **%d người** (tổng %d hồ sơ):\n", len(order), len(flat))
	}

	schemas := make([]string, 0, len(schemaSet))
	for s := range schemaSet {
		schemas = append(schemas, s)
	}
	sort.Strings(schemas)
	return persons, header + strings.Join(blocks, "\n"), schemas
}
