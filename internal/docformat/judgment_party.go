package docformat

import (
	"regexp"
	"strings"
)

// Judgment kinds of a văn bản của Đảng (Hướng dẫn 36-HD/VPTW, party.go),
// items 2–5 of its Bảng kiểm (skills/the-thuc-dang):
//
//   - dang_tham_quyen (item 2): quyền hạn ký written "T/M", "K/T", "T/L"
//     (bad_abbrev_regex — the NĐ30 "TM.", "KT.", "TL." — fails); under
//     "K/T <chức vụ>" the line is the deputy's "PHÓ <same chức vụ>"; the
//     văn bản of a collective body (collective_regex on the last cơ quan
//     ban hành line: cấp uỷ, uỷ ban kiểm tra, đảng đoàn, ban cán sự đảng,
//     hội đồng, đại hội …) needs a "T/M" line unless tm_exempt_regex (T/L)
//     matches one. A "PHÓ …" line without "K/T" is no finding: a deputy
//     signs directly when no head is appointed, or under "T/M".
//   - dang_chuc_vu (item 3): a chức vụ line carrying a cấp uỷ name
//     (agency_regex: "PHÓ BÍ THƯ TỈNH UỶ") warns, except with several
//     signature blocks or a "kiêm" line (liên cơ quan, ban chỉ đạo); an
//     "ỦY VIÊN" line under "T/M" warns; a học hàm, học vị, quân hàm before
//     the họ tên (honorific_regex) fails.
//   - dang_noi_nhan (item 4): label "Nơi nhận:", entries "- …" ending in
//     "," (end_regex), the last in "." (last_end_regex), a "- Lưu …" line
//     without a colon after "Lưu" (luu_colon_regex).
//   - dang_so_ky_hieu_date (item 5): "Số 05-NQ/TU" — no colon, the số
//     with two digits at least, "-" before the type code, which matches
//     the loại; the địa danh line as for NĐ30 (judgeDate).

var (
	partySlashRe   = regexp.MustCompile(`^(T/M|K/T|T/L)\s+\S`)
	partyTMRe      = regexp.MustCompile(`^T/M\s+\S`)
	partyKTRe      = regexp.MustCompile(`^K/T\s+\S`)
	partyAuthRe    = regexp.MustCompile(`^(T/M|K/T|T/L|Q\.)`)
	partyKiemRe    = regexp.MustCompile(`^KIEM$`)
	partyDaiHoiRe  = regexp.MustCompile(`^DAI HOI\b`)
	partySoLineRe  = regexp.MustCompile(`^SO\s*(:)?\s*([^-/]*?)\s*-\s*([A-Z]+)\s*/\s*(\S.*)$`)
	nd30SoLineRe   = regexp.MustCompile(`^SO\s*:?\s*[^/]*/`)
	nd30AbbrevPart = regexp.MustCompile(`^(TM|KT|TL|TUQ)\s*\.\s*|^(T\.M|K\.T|T\.L)\s*`)
)

// partySlashFix rewrites a quyền hạn line in the HD36 form: "TM. BAN
// THƯỜNG VỤ" → "T/M BAN THƯỜNG VỤ".
func partySlashFix(raw string) string {
	f := fold(raw)
	m := nd30AbbrevPart.FindStringSubmatch(f)
	if m == nil {
		return raw
	}
	code := m[1] + m[2]
	slash := map[string]string{"TM": "T/M", "KT": "K/T", "TL": "T/L", "TUQ": "T/L", "T.M": "T/M", "K.T": "K/T", "T.L": "T/L"}[code]
	rest := strings.TrimSpace(string([]rune(strings.TrimSpace(raw))[runeLen(m[0]):]))
	return strings.TrimSpace(slash + " " + rest)
}

func (jd *judgeData) dangThamQuyen(jr JudgmentRule) ruleOut {
	var o ruleOut
	blocks := jd.signatureBlocks()
	if len(blocks) == 0 {
		if jd.found("nguoi_ky") {
			o.undetermined = "khối ký không tách được dòng quyền hạn, chức vụ"
		} else {
			o.unknown = "không có khối ký (xem mục 1)"
		}
		return o
	}
	const comp = "chuc_danh"
	bad := jre(jr.BadAbbrevRegex)
	hasTM, exempt := false, false
	ex := jre(jr.TMExemptRegex)
	var first jline
	for _, b := range blocks {
		for i, l := range b {
			if first.raw == "" {
				first = l
			}
			// a "TM." line is flagged below; it still shows the T/M intent
			hasTM = hasTM || partyTMRe.MatchString(l.f) || tmLineRe.MatchString(l.f)
			exempt = exempt || (ex != nil && ex.MatchString(l.f))
			if bad != nil && bad.MatchString(l.f) {
				fix := partySlashFix(l.raw)
				o.add("error", comp, "Quyền hạn ký văn bản của Đảng viết T/M (thay mặt), K/T (ký thay), T/L (thừa lệnh) — "+PartyGuide+" mục 1.7.1",
					l.raw, "Sửa thành "+quote(fix)+".", quote(l.raw)+" viết sai quyền hạn ký (phải là "+quote(fix)+")")
				continue
			}
			if partySlashRe.MatchString(l.f) {
				prefix := string([]rune(l.raw)[:3])
				if strings.ToUpper(prefix) != prefix {
					o.add("error", comp, "Quyền hạn ký in hoa: T/M, K/T, T/L", l.raw,
						"Sửa thành "+quote(strings.ToUpper(prefix)+string([]rune(l.raw)[3:]))+".", quote(l.raw)+" viết thường")
				}
			}
			if !partyKTRe.MatchString(l.f) {
				continue
			}
			title := strings.TrimSpace(string([]rune(l.raw)[3:]))
			if i+1 >= len(b) || !phoLineRe.MatchString(b[i+1].f) {
				o.add("error", comp, "Cấp phó ký thay cấp trưởng ghi \"K/T <chức vụ cấp trưởng>\" rồi dòng dưới là chức vụ cấp phó — "+PartyGuide+" mục 1.7.1",
					l.raw, "Ghi dòng "+quote("PHÓ "+title)+" ngay dưới "+quote(l.raw)+".",
					quote(l.raw)+" không có dòng chức vụ cấp phó ngay dưới")
				continue
			}
			if pho := titleAfter(b[i+1].raw); !sameTitle(fold(title), fold(pho)) {
				o.add("error", comp, "Chức vụ sau \"K/T\" phải trùng chức vụ cấp trưởng mà cấp phó ký thay",
					l.raw+" / "+b[i+1].raw, "Sửa thành "+quote("K/T "+pho)+".", quote(l.raw)+" không khớp "+quote(b[i+1].raw))
			}
		}
	}
	// collective bodies sign "T/M" — but a "kiêm" signer (ban chỉ đạo,
	// hội đồng without a seal of their own), and a deputy of a hội đồng,
	// ban chỉ đạo or tiểu ban, signs without it (HD36 1.7.1 đ)
	kiem := false
	for _, b := range blocks {
		for _, l := range b {
			kiem = kiem || partyKiemRe.MatchString(l.f)
		}
	}
	if cre := jre(jr.CollectiveRegex); cre != nil && !hasTM && !exempt && !kiem {
		agency := jd.lines("co_quan_ban_hanh")
		collective := false
		if n := len(agency); n > 0 {
			collective = cre.MatchString(agency[n-1].f) || partyDaiHoiRe.MatchString(agency[0].f)
			if dre := jre(jr.DeputyExemptRegex); dre != nil && dre.MatchString(agency[n-1].f) &&
				phoLineRe.MatchString(first.f) {
				collective = false
			}
		}
		if collective {
			sev := jr.TMSeverity
			if sev == "" {
				sev = "error"
			}
			o.add(sev, comp, "Văn bản của cấp uỷ, uỷ ban kiểm tra, đảng đoàn, ban cán sự đảng, hội đồng, đại hội đảng ký thay mặt tập thể: \"T/M <tên tập thể>\" — "+PartyGuide+" mục 1.7.1",
				first.raw, "Thêm dòng \"T/M <tên tập thể>\" (ví dụ \"T/M BAN THƯỜNG VỤ\") trên chức vụ người ký.",
				"thiếu \"T/M\" ("+quote(first.raw)+")")
		}
	}
	return o
}

func (jd *judgeData) dangChucVu(jr JudgmentRule) ruleOut {
	var o ruleOut
	blocks := jd.signatureBlocks()
	if len(blocks) == 0 && !jd.found("nguoi_ky") {
		o.unknown = "không có khối ký (xem mục 1)"
		return o
	}
	agency, member := jre(jr.AgencyRegex), jre(jr.MemberRegex)
	kiem := false
	for _, b := range blocks {
		for _, l := range b {
			kiem = kiem || partyKiemRe.MatchString(l.f)
		}
	}
	for _, b := range blocks {
		tm := false
		for _, l := range b {
			if partyAuthRe.MatchString(l.f) || nd30AbbrevPart.MatchString(l.f) {
				// a quyền hạn line (a wrong "TM." is item 2's)
				tm = tm || partyTMRe.MatchString(l.f)
				continue
			}
			if agency != nil && len(blocks) == 1 && !kiem && agency.MatchString(l.f) {
				o.add("warn", "chuc_danh", "Chức vụ người ký không ghi kèm tên cấp uỷ, cơ quan (ghi \"PHÓ BÍ THƯ\", không ghi \"PHÓ BÍ THƯ TỈNH UỶ\") — "+PartyGuide+" mục 1.7.1",
					l.raw, "Bỏ tên cấp uỷ, cơ quan khỏi dòng chức vụ.", quote(l.raw)+" ghi kèm tên cấp uỷ")
			}
			if member != nil && tm && member.MatchString(l.f) {
				o.add("warn", "chuc_danh", "Khi ký thay mặt (T/M) không ghi chức vụ uỷ viên của người ký", l.raw,
					"Bỏ dòng "+quote(l.raw)+".", quote(l.raw)+" dưới T/M")
			}
		}
	}
	if hre := jre(jr.HonorificRegex); hre != nil {
		for _, l := range jd.lines("nguoi_ky") {
			if hre.MatchString(l.f) {
				o.add("error", "nguoi_ky", "Không ghi học hàm, học vị, quân hàm, danh hiệu trước họ tên người ký — "+PartyGuide+" mục 1.7.1",
					l.raw, "Chỉ ghi họ và tên.", quote(l.raw)+" có học hàm, học vị hoặc quân hàm")
			}
		}
	}
	return o
}

func (jd *judgeData) dangNoiNhan(jr JudgmentRule) ruleOut {
	var o ruleOut
	const comp = "noi_nhan"
	lines := jd.lines(comp)
	if len(lines) == 0 {
		if rs, err := RuleSetForReport(jd.r); err == nil && !hasString(rs.RequiredComponents, comp) {
			o.note = "không áp dụng (loại này không bắt buộc nơi nhận)"
			return o
		}
		o.unknown = "không có Nơi nhận (xem mục 1)"
		return o
	}
	labelRe, itemRe, endRe, lastRe := jre(jr.LabelRegex), jre(jr.ItemRegex), jre(jr.EndRegex), jre(jr.LastEndRegex)
	luuRe, colonRe := jre(jr.LuuRegex), jre(jr.LuuColonRegex)
	body := lines
	if strings.HasPrefix(lines[0].f, "NOI NHAN") {
		body = lines[1:]
		if labelRe != nil && !labelRe.MatchString(lines[0].f) {
			o.add("error", comp, "Dòng đầu nơi nhận ghi \"Nơi nhận:\" (có dấu hai chấm)", lines[0].raw,
				"Sửa thành \"Nơi nhận:\".", quote(lines[0].raw)+" sai nhãn")
		}
	} else {
		o.add("error", comp, "Nơi nhận bắt đầu bằng dòng \"Nơi nhận:\"", lines[0].raw,
			"Thêm dòng \"Nơi nhận:\" phía trên danh sách.", "thiếu nhãn \"Nơi nhận:\"")
	}
	// entries: a line without a dash continues the entry above it, even
	// after an inner comma ("- Các ban, đảng uỷ," / "hội đồng trực thuộc,")
	type entry struct{ raw, f string }
	var entries []entry
	var noDash []string
	for _, l := range body {
		dashed := itemRe == nil || itemRe.MatchString(l.f)
		if !dashed && len(entries) > 0 {
			e := &entries[len(entries)-1]
			e.raw += " " + l.raw
			e.f += " " + l.f
			continue
		}
		if !dashed {
			noDash = append(noDash, l.raw)
		}
		entries = append(entries, entry{l.raw, l.f})
	}
	if len(noDash) > 0 {
		o.add("error", comp, "Mỗi nơi nhận một dòng, bắt đầu bằng gạch đầu dòng \"- \"",
			strings.Join(noDash, " | "), "Thêm \"- \" ở đầu dòng.", quote(noDash[0])+" không bắt đầu bằng \"- \"")
	}
	var noEnd []string
	for i, e := range entries {
		want := endRe
		if i == len(entries)-1 {
			want = lastRe
		}
		if want != nil && !want.MatchString(e.f) {
			noEnd = append(noEnd, e.raw)
		}
	}
	if len(noEnd) > 0 {
		o.add("error", comp, "Sau mỗi nơi nhận có dấu phẩy (,), kết thúc nơi nhận có dấu chấm (.) — "+PartyGuide+" mục 1.9.2",
			strings.Join(noEnd, " | "), "Kết thúc mỗi dòng bằng \",\", dòng cuối bằng \".\".",
			quote(noEnd[0])+" sai dấu kết thúc (dòng thường \",\", dòng cuối \".\")")
	}
	luu := -1
	for i, e := range entries {
		if luuRe != nil && luuRe.MatchString(e.f) {
			luu = i
		}
	}
	switch {
	case luu < 0:
		o.add("error", comp, "Nơi nhận có dòng lưu, ví dụ \"- Lưu Văn phòng Tỉnh uỷ.\"", "",
			"Thêm dòng \"- Lưu Văn phòng <cấp uỷ>.\" ở cuối nơi nhận.", "thiếu dòng \"- Lưu …\"")
	case colonRe != nil && colonRe.MatchString(entries[luu].f):
		rest := strings.TrimSpace(lastAfterColon(entries[luu].raw))
		o.add("error", comp, "Dòng lưu của văn bản của Đảng không có dấu hai chấm: \"- Lưu Văn phòng Tỉnh uỷ.\"",
			entries[luu].raw, "Sửa thành "+quote("- Lưu "+rest)+".", quote(entries[luu].raw)+" có dấu hai chấm sau \"Lưu\"")
	}
	return o
}

func lastRune(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) == 0 {
		return ""
	}
	return string(r[len(r)-1])
}

func (jd *judgeData) dangSoKyHieuDate(jr JudgmentRule) ruleOut {
	var o ruleOut
	var notes []string
	if lines := jd.lines("so_ky_hieu"); len(lines) > 0 {
		jd.judgePartySo(lines[0], &o, &notes)
	}
	if lines := jd.lines("dia_danh_ngay_thang"); len(lines) > 0 {
		jd.judgeDate(lines[0], &o, &notes)
	}
	if !jd.found("so_ky_hieu") && !jd.found("dia_danh_ngay_thang") {
		notes = append(notes, "không có số ký hiệu, ngày tháng")
	}
	o.note = strings.Join(notes, "; ")
	return o
}

func (jd *judgeData) judgePartySo(l jline, o *ruleOut, notes *[]string) {
	const comp = "so_ky_hieu"
	form := "\"Số <số>-<ký hiệu tên loại>/<ký hiệu cơ quan>\" (ví dụ \"Số 15-HD/TU\")"
	m := partySoLineRe.FindStringSubmatch(l.f)
	if m == nil {
		if nd30SoLineRe.MatchString(l.f) {
			o.add("error", comp, "Số và ký hiệu văn bản của Đảng ghi "+form+" — "+PartyGuide+" mục 1.3", l.raw,
				"Sửa theo dạng "+form+".", quote(l.raw)+" ghi theo dạng của văn bản hành chính")
			return
		}
		o.undetermined = "không đọc được số, ký hiệu " + quote(l.raw)
		return
	}
	if m[1] != "" {
		fixed := strings.TrimSpace(strings.Replace(l.raw, ":", "", 1))
		o.add("error", comp, "Sau chữ \"Số\" không có dấu hai chấm: "+form, l.raw,
			"Sửa thành "+quote(strings.Join(strings.Fields(fixed), " "))+".", quote(l.raw)+" có dấu hai chấm sau \"Số\"")
	}
	num := strings.TrimSpace(m[2])
	switch {
	case soPlaceRe.MatchString(num):
		*notes = append(*notes, "chưa có số")
	case !digitsOnlyRe.MatchString(num):
		o.undetermined = "không đọc được số " + quote(l.raw)
		return
	case len(num) < 2:
		o.add("error", comp, "Số văn bản nhỏ hơn 10 ghi số 0 phía trước", l.raw,
			"Sửa thành \"Số 0"+num+"-…\".", quote(l.raw)+" thiếu số 0 trước số "+num)
	}
	docType := ""
	if jd.r.DocumentType != nil {
		docType = jd.r.DocumentType.Used
	}
	t := partyTypeBySlug(docType)
	if t == nil {
		return
	}
	for _, c := range t.Codes {
		if c == m[3] {
			return
		}
	}
	want := strings.NewReplacer("QDI", "QĐi", "QD", "QĐ", "CTR", "CTr", "TTR", "TTr").Replace(t.Codes[0])
	o.add("error", comp, "Ký hiệu tên loại phải khớp tên loại văn bản ("+t.Name+": "+want+")", l.raw,
		"Sửa ký hiệu loại thành \""+want+"\".", "ký hiệu loại \""+m[3]+"\" không khớp "+t.Name)
}
