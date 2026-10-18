package tools

import (
	"context"
	"strings"
	"testing"
)

func readyFields(t *testing.T, id string, draft map[string]any, inputs ...string) map[string]interface{} {
	t.Helper()
	out, err := PrepareDocumentTemplate(context.Background(), templateInput(&scriptedChat{replies: []string{draftJSON(t, draft)}}, id, inputs...))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Ready {
		t.Fatalf("%s not ready: %+v", id, out.Data)
	}
	return out.Data["fields"].(map[string]interface{})
}

func bodyTexts(f map[string]interface{}) []string {
	var out []string
	for _, p := range f["paragraphs"].([]templateParagraph) {
		out = append(out, p.Text)
	}
	return out
}

func directSigner() map[string]string {
	return map[string]string{"name": "Nguyễn Văn A", "position": "Giám đốc", "authority": "truc_tiep"}
}

func TestDocumentTemplateInvitation(t *testing.T) {
	f := readyFields(t, "giay-moi", map[string]any{
		"subject":            "Hội nghị triển khai chuyển đổi số năm 2026",
		"agency_in_sentence": "Sở Nội vụ",
		"form": map[string]string{
			"invitee": "Đại diện lãnh đạo các phòng thuộc Sở", "event": "Hội nghị triển khai chuyển đổi số năm 2026",
			"chair": "Đồng chí Giám đốc Sở", "time": "08 giờ 00, ngày 20/10/2026", "venue": "Hội trường tầng 3, Sở Nội vụ",
		},
		"paragraphs": []map[string]string{{"kind": "body", "text": "Đề nghị đại biểu mang theo báo cáo của đơn vị."}},
		"recipients": []string{"- Như trên;"},
		"signer":     directSigner(),
		"addressee":  "Các phòng thuộc Sở",
	}, "Giấy mời hội nghị triển khai chuyển đổi số năm 2026 do đồng chí Giám đốc Sở chủ trì, 08 giờ 00 ngày 20/10/2026 tại Hội trường tầng 3",
		"Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp")
	// a giấy mời has no "Kính gửi"
	if _, ok := f["addressee"]; f["number_symbol"] != "      /GM-SNV" || ok {
		t.Fatalf("fields: %+v", f)
	}
	want := []string{
		"Sở Nội vụ trân trọng kính mời: Đại diện lãnh đạo các phòng thuộc Sở.",
		"Tới dự Hội nghị triển khai chuyển đổi số năm 2026.",
		"Chủ trì: Đồng chí Giám đốc Sở.",
		"Thời gian: 08 giờ 00, ngày 20/10/2026.",
		"Địa điểm: Hội trường tầng 3, Sở Nội vụ.",
		"Đề nghị đại biểu mang theo báo cáo của đơn vị./.",
	}
	if got := bodyTexts(f); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("body:\n%s", strings.Join(got, "\n"))
	}
	// "Như trên": those invited
	if r := f["recipients"].([]string); strings.Join(r, "|") != "- Như trên;|- Lưu: VT, VP." {
		t.Fatalf("recipients: %v", r)
	}
}

func TestDocumentTemplateInvitationAsksForTheTime(t *testing.T) {
	out, err := PrepareDocumentTemplate(context.Background(), templateInput(&scriptedChat{replies: []string{draftJSON(t, map[string]any{
		"subject": "Họp giao ban",
		"form":    map[string]string{"invitee": "Các phòng", "event": "Họp giao ban", "venue": "Phòng họp số 1"},
		"signer":  directSigner(),
	})}}, "giay-moi", "Giấy mời họp giao ban ở phòng họp số 1", "Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp"))
	if err != nil {
		t.Fatal(err)
	}
	missing := out.Data["missing_fields"].([]string)
	if out.Ready || len(missing) != 1 || !strings.HasPrefix(missing[0], "Thời gian") {
		t.Fatalf("want only the time asked: %+v", out.Data)
	}
	// skipped: the time is left blank, nothing else changes
	out, err = PrepareDocumentTemplate(context.Background(), templateInput(&scriptedChat{replies: []string{draftJSON(t, map[string]any{
		"subject": "Họp giao ban",
		"form":    map[string]string{"invitee": "Các phòng", "event": "Họp giao ban", "venue": "Phòng họp số 1"},
		"signer":  directSigner(),
	})}}, "giay-moi", "Giấy mời họp giao ban ở phòng họp số 1", "Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp", "bỏ qua"))
	if err != nil || !out.Ready {
		t.Fatalf("not ready: %+v %v", out.Data, err)
	}
	body := bodyTexts(out.Data["fields"].(map[string]interface{}))
	if body[2] != "Thời gian: "+templateBlank+"." || body[len(body)-1] != "Địa điểm: Phòng họp số 1./." {
		t.Fatalf("body: %q", body)
	}
}

func TestDocumentTemplateIntroductionKeepsOnlyStatedNames(t *testing.T) {
	draft := map[string]any{
		"form": map[string]string{
			"person": "Trần Thị B", "position": "Chuyên viên Văn phòng", "destination": "Sở Tài chính",
			"purpose": "Liên hệ công tác quyết toán", "valid_until": "31/10/2026",
		},
		"signer": directSigner(),
	}
	out, err := PrepareDocumentTemplate(context.Background(), templateInput(&scriptedChat{replies: []string{draftJSON(t, draft)}}, "giay-gioi-thieu",
		"Giới thiệu chuyên viên Văn phòng sang Sở Tài chính liên hệ quyết toán, giá trị đến 31/10/2026",
		"Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Ready || !strings.Contains(strings.Join(out.Data["missing_fields"].([]string), "|"), "Họ tên người được giới thiệu") {
		t.Fatalf("an unstated name must be asked: %+v", out.Data)
	}
	f := readyFields(t, "giay-gioi-thieu", draft,
		"Giới thiệu bà Trần Thị B, chuyên viên Văn phòng, sang Sở Tài chính liên hệ công tác quyết toán, giá trị đến 31/10/2026",
		"Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp")
	want := []string{
		"Sở Nội vụ trân trọng giới thiệu:",
		"Ông (bà): Trần Thị B",
		"Chức vụ: Chuyên viên Văn phòng",
		"Được cử đến: Sở Tài chính",
		"Về việc: Liên hệ công tác quyết toán",
		"Đề nghị Quý cơ quan tạo điều kiện để ông (bà) có tên ở trên hoàn thành nhiệm vụ.",
		"Giấy này có giá trị đến hết ngày 31/10/2026./.",
	}
	if got := bodyTexts(f); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("body:\n%s", strings.Join(got, "\n"))
	}
	if f["number_symbol"] != "      /GGT-SNV" || f["subject"] != "ông (bà) Trần Thị B" {
		t.Fatalf("fields: %+v", f)
	}
	// Mẫu 1.8: kept at the văn thư only
	if r := f["recipients"].([]string); strings.Join(r, "|") != "- Như trên;|- Lưu: VT." {
		t.Fatalf("recipients: %v", r)
	}
}

func TestDocumentTemplateFormFactsNotStatedAreAsked(t *testing.T) {
	// the chair the user never named, a sender title the model made up
	f := readyFields(t, "giay-moi", map[string]any{
		"subject": "Họp giao ban",
		"form":    map[string]any{"invitee": "Các phòng", "event": "Họp giao ban", "chair": "Giám đốc Sở", "time": "8 giờ", "venue": "Phòng họp số 1"},
		"signer":  directSigner(),
	}, "Giấy mời các phòng họp giao ban 8 giờ ở phòng họp số 1", "Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp")
	if body := strings.Join(bodyTexts(f), "|"); strings.Contains(body, "Chủ trì") {
		t.Fatalf("an unstated chair printed: %s", body)
	}
	f = readyFields(t, "cong-dien", map[string]any{
		"subject":    "Ứng phó bão",
		"form":       map[string]any{"sender": "Chủ tịch Ủy ban nhân dân tỉnh", "receivers": []string{"Các phòng", "Các đơn vị"}},
		"paragraphs": []map[string]string{{"kind": "body", "text": "Trực ban."}},
		"signer":     directSigner(),
	}, "Công điện ứng phó bão gửi các phòng, các đơn vị: trực ban", "Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp")
	// the agency itself sends it; a list of receivers is read as one
	if f["issuer_line"] != "SỞ NỘI VỤ điện:" || strings.Join(f["receiver_lines"].([]string), "|") != "- Các phòng;|- Các đơn vị;" {
		t.Fatalf("fields: %+v", f)
	}
}

func TestDocumentTemplateFormNumbersFromTheModel(t *testing.T) {
	f := readyFields(t, "giay-nghi-phep", map[string]any{
		"form": map[string]any{
			"request_date": "10/10/2026", "person": "Lê Văn C", "position": "Chuyên viên", "duration": 5,
			"from": "19/10/2026", "to": "23/10/2026", "place": "Huế", "counted_as": "nghỉ phép năm 2026",
		},
		"signer": directSigner(),
	}, "Giấy nghỉ phép cho ông Lê Văn C, chuyên viên, đơn ngày 10/10/2026, nghỉ 5 ngày từ 19/10/2026 đến 23/10/2026 tại Huế, tính vào phép năm 2026",
		"Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp")
	if body := strings.Join(bodyTexts(f), "|"); !strings.Contains(body, "trong thời gian 5,") {
		t.Fatalf("body: %s", body)
	}
}

func TestDocumentTemplateLeave(t *testing.T) {
	f := readyFields(t, "giay-nghi-phep", map[string]any{
		"form": map[string]string{
			"request_date": "10/10/2026", "person": "Lê Văn C", "position": "Chuyên viên Phòng Tổ chức",
			"duration": "05 ngày", "from": "19/10/2026", "to": "23/10/2026", "place": "thành phố Đà Nẵng",
			"counted_as": "nghỉ phép năm 2026",
		},
		"signer": directSigner(),
	}, "Giấy nghỉ phép cho ông Lê Văn C, chuyên viên Phòng Tổ chức, đơn ngày 10/10/2026, nghỉ 05 ngày từ 19/10/2026 đến 23/10/2026 tại thành phố Đà Nẵng, tính vào phép năm 2026",
		"Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp")
	want := []string{
		"Xét Đơn đề nghị nghỉ phép ngày 10/10/2026 của ông (bà) Lê Văn C,",
		"Sở Nội vụ cấp cho:",
		"Ông (bà): Lê Văn C",
		"Chức vụ: Chuyên viên Phòng Tổ chức",
		"Được nghỉ phép trong thời gian 05 ngày, kể từ ngày 19/10/2026 đến hết ngày 23/10/2026 tại thành phố Đà Nẵng.",
		"Số ngày nghỉ phép nêu trên được tính vào thời gian nghỉ phép năm 2026./.",
	}
	if got := bodyTexts(f); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("body:\n%s", strings.Join(got, "\n"))
	}
	if r := f["recipients"].([]string); strings.Join(r, "|") != "- Ông (bà) Lê Văn C;|- Lưu: VT, VP." || f["number_symbol"] != "      /GNP-SNV" {
		t.Fatalf("fields: %+v", f)
	}
}

func TestDocumentTemplateMinutes(t *testing.T) {
	f := readyFields(t, "bien-ban", map[string]any{
		"subject": "Cuộc họp giao ban tháng 10",
		"form": map[string]string{
			"start_time": "14 giờ 00, ngày 15/10/2026", "venue": "Phòng họp số 1", "participants": "Lãnh đạo Sở và trưởng các phòng",
			"chair_name": "Nguyễn Văn A", "chair_position": "Giám đốc Sở", "secretary_name": "Phạm Thị D", "secretary_position": "Chuyên viên Văn phòng",
		},
		"paragraphs": []map[string]string{{"kind": "body", "text": "Thành phần tham dự: Lãnh đạo Sở. Nội dung diễn biến: các phòng báo cáo kết quả tháng 10."}, {"kind": "body", "text": "Giám đốc kết luận giao Văn phòng tổng hợp./."}, {"kind": "body", "text": "Cuộc họp kết thúc cùng ngày./."}},
		"recipients": []string{},
		"signer":     map[string]string{"name": "Nguyễn Văn A", "position": "Giám đốc"},
	}, "Biên bản cuộc họp giao ban tháng 10, bắt đầu 14 giờ 00 ngày 15/10/2026 tại phòng họp số 1, chủ trì Nguyễn Văn A, Giám đốc Sở, thư ký Phạm Thị D")
	body := bodyTexts(f)
	// the proceedings do not repeat the form's lines
	if body[3] != "Chủ trì (chủ tọa): Nguyễn Văn A - Giám đốc Sở." || body[5] != "Nội dung (theo diễn biến cuộc họp):" || body[6] != "Các phòng báo cáo kết quả tháng 10." {
		t.Fatalf("body: %q", body)
	}
	// not over yet: the end is left for the secretary
	if last := body[len(body)-1]; last != "Cuộc họp kết thúc vào "+templateBlank+" giờ "+templateBlank+", ngày "+templateBlank+" tháng "+templateBlank+" năm "+templateBlank+"./." {
		t.Fatalf("end: %q", last)
	}
	// signed by the chair and the secretary, no form of signing asked
	if f["signer_name"] != "Nguyễn Văn A" || f["secretary_name"] != "Phạm Thị D" || strings.Join(f["signature_lines"].([]string), "|") != "CHỦ TỌA" {
		t.Fatalf("signature: %+v", f)
	}
	// kept on file: not sent to the profile's usual recipients
	if body[7] != "Giám đốc kết luận giao Văn phòng tổng hợp." {
		t.Fatalf("body: %q", body)
	}
	if r := f["recipients"].([]string); strings.Join(r, "|") != "- Lưu: VT, Hồ sơ." || f["number_symbol"] != "      /BB-SNV" {
		t.Fatalf("fields: %+v", f)
	}
}

func TestDocumentTemplateTelegram(t *testing.T) {
	f := readyFields(t, "cong-dien", map[string]any{
		"subject":    "Chủ động ứng phó với bão số 5",
		"form":       map[string]string{"sender": "Giám đốc Sở Nội vụ", "receivers": "Trưởng các phòng thuộc Sở; Thủ trưởng các đơn vị sự nghiệp"},
		"paragraphs": []map[string]string{{"kind": "body", "text": "Yêu cầu các đơn vị trực 24/24 giờ."}},
		"recipients": []string{"- Trưởng các phòng thuộc Sở;", "- Thủ trưởng các đơn vị sự nghiệp;", "- UBND tỉnh (để b/c);"},
		"signer":     directSigner(),
	}, "Công điện về chủ động ứng phó bão số 5 gửi trưởng các phòng thuộc Sở, thủ trưởng các đơn vị sự nghiệp, trực 24/24 giờ",
		"Giám đốc Sở Nội vụ điện; người ký Nguyễn Văn A, Giám đốc, ký trực tiếp")
	if f["issuer_line"] != "GIÁM ĐỐC SỞ NỘI VỤ điện:" {
		t.Fatalf("issuer: %v", f["issuer_line"])
	}
	if r := f["receiver_lines"].([]string); strings.Join(r, "|") != "- Trưởng các phòng thuộc Sở;|- Thủ trưởng các đơn vị sự nghiệp;" {
		t.Fatalf("receivers: %v", r)
	}
	if body := bodyTexts(f); len(body) != 1 || body[0] != "Yêu cầu các đơn vị trực 24/24 giờ./." {
		t.Fatalf("body: %q", body)
	}
	// the receivers are "Như trên", not listed again
	if r := f["recipients"].([]string); strings.Join(r, "|") != "- Như trên;|- UBND tỉnh (để b/c);|- Lưu: VT, VP." || f["number_symbol"] != "      /CĐ-SNV" {
		t.Fatalf("fields: %+v", f)
	}
}

func TestDocumentTemplateResolution(t *testing.T) {
	f := readyFields(t, "nghi-quyet", map[string]any{
		"subject": "Thông qua kế hoạch chuyển đổi số năm 2026",
		"grounds": []string{"Căn cứ Luật Tổ chức chính quyền địa phương"},
		"paragraphs": []map[string]string{
			{"kind": "article", "text": "Điều 1. Thông qua kế hoạch"}, {"kind": "body", "text": "Thông qua kế hoạch chuyển đổi số năm 2026."},
			{"kind": "article", "text": "Điều 2. Tổ chức thực hiện"}, {"kind": "article", "text": "Nghị quyết có hiệu lực từ ngày ký."},
		},
		"signer": map[string]string{"name": "Nguyễn Văn A", "position": "Giám đốc", "authority": "TM", "on_behalf_of": "Ban Giám đốc"},
	}, "Nghị quyết thông qua kế hoạch chuyển đổi số năm 2026, căn cứ Luật Tổ chức chính quyền địa phương",
		"Người ký: Nguyễn Văn A, Giám đốc, TM. Ban Giám đốc")
	// the collective signing TM. resolves
	if f["decision_authority"] != "BAN GIÁM ĐỐC SỞ NỘI VỤ" || f["number_symbol"] != "      /NQ-SNV" {
		t.Fatalf("fields: %+v", f)
	}
	// a paragraph marked "article" that is no "Điều N." is body text
	if p := f["paragraphs"].([]templateParagraph); p[2].Kind != "body" {
		t.Fatalf("paragraphs: %+v", p)
	}
	// no proposal stated: no line, and nothing asked for it
	if _, ok := f["request_basis"]; ok {
		t.Fatalf("request_basis: %v", f["request_basis"])
	}
	if s := f["signature_lines"].([]string); strings.Join(s, "|") != "TM. BAN GIÁM ĐỐC|GIÁM ĐỐC" {
		t.Fatalf("signature: %v", s)
	}
}

func TestExpandAgencyAbbr(t *testing.T) {
	// "TM. Ủy ban nhân dân tỉnh" of the agency saved as "UBND TỈNH HÀ TĨNH"
	if !strings.Contains(expandAgencyAbbr("Ủy ban nhân dân tỉnh Hà Tĩnh"), expandAgencyAbbr("UBND TỈNH HÀ TĨNH")) ||
		!strings.Contains(expandAgencyAbbr("Uỷ ban nhân dân tỉnh Hà Tĩnh"), expandAgencyAbbr("Ủy ban nhân dân tỉnh Hà Tĩnh")) {
		t.Fatal("UBND / uỷ not folded")
	}
}

func TestDocumentTemplateDirective(t *testing.T) {
	f := readyFields(t, "chi-thi", map[string]any{
		"subject":    "Tăng cường kỷ luật hành chính",
		"paragraphs": []map[string]string{{"kind": "body", "text": "Yêu cầu các phòng nghiêm túc thực hiện Chỉ thị này./."}},
		"signer":     directSigner(),
	}, "Chỉ thị tăng cường kỷ luật hành chính", "Người ký: Nguyễn Văn A, Giám đốc, ký trực tiếp")
	if f["number_symbol"] != "      /CT-SNV" {
		t.Fatalf("number: %v", f["number_symbol"])
	}
}

func TestAgencyInSentence(t *testing.T) {
	for _, c := range []struct{ agency, model, want string }{
		{"SỞ NỘI VỤ", "Sở Nội vụ", "Sở Nội vụ"},
		{"SỞ NỘI VỤ", "Sở Nội vụ tỉnh Huế", "Sở Nội vụ"},
		{"SỞ NỘI VỤ", "", "Sở Nội vụ"},
		{"ỦY BAN NHÂN DÂN XÃ AN HÒA", "", "Ủy ban nhân dân xã An Hòa"},
		{"CÔNG AN THÀNH PHỐ HUẾ", "", "Công an thành phố Huế"},
		{"PHÒNG TỔ CHỨC CÁN BỘ", "", "Phòng Tổ chức cán bộ"},
		{"SỞ TÀI NGUYÊN VÀ MÔI TRƯỜNG", "", "Sở Tài nguyên và Môi trường"},
		{"Sở Nội vụ", "SỞ NỘI VỤ", "Sở Nội vụ"},
	} {
		if got := agencyInSentence(c.agency, c.model); got != c.want {
			t.Errorf("agencyInSentence(%q, %q) = %q, want %q", c.agency, c.model, got, c.want)
		}
	}
}
