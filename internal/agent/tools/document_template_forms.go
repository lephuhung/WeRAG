package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The NĐ30 forms whose body is a set of labelled lines (Phụ lục III, Mẫu
// 1.6–1.10: công điện, giấy mời, giấy giới thiệu, biên bản, giấy nghỉ
// phép). The model gives the facts in draft.Form, by key; code writes the
// lines of the form around them, so the fixed wording ("trân trọng kính
// mời", "Đề nghị Quý cơ quan tạo điều kiện …", "Giấy này có giá trị đến hết
// ngày …") is the decree's, never the model's.

// formField is one fact of a form.
type formField struct {
	key string
	// label is the question asked when the fact is missing.
	label string
	// hint tells the model what the value is.
	hint     string
	optional bool
	// stated: a name or title, kept only when the user wrote it.
	stated bool
}

// formValue is one fact as the model gave it: a string, or a number or a
// list it wrote instead ("duration": 5, "receivers": ["A", "B"]).
type formValue string

func (v *formValue) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*v = formValue(s)
		return nil
	}
	var list []interface{}
	if err := json.Unmarshal(b, &list); err == nil {
		var parts []string
		for _, x := range list {
			if t := strings.TrimSpace(fmt.Sprint(x)); t != "" && x != nil {
				parts = append(parts, t)
			}
		}
		*v = formValue(strings.Join(parts, "; "))
		return nil
	}
	var x interface{}
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	if x != nil {
		*v = formValue(fmt.Sprint(x))
	}
	return nil
}

// templateForms are the facts of each form type, in the order asked.
var templateForms = map[string][]formField{
	"giay-moi": {
		{key: "invitee", label: "Người hoặc cơ quan được mời (họ tên, chức vụ, đơn vị)", hint: "who is invited: agency, or name, position and unit"},
		{key: "event", label: "Tên (nội dung) cuộc họp, hội nghị được mời dự", hint: "the meeting / conference attended, after \"Tới dự\""},
		{key: "chair", label: "Người chủ trì", hint: "who chairs it", optional: true, stated: true},
		{key: "time", label: "Thời gian (giờ, ngày)", hint: "time and date as the user wrote them"},
		{key: "venue", label: "Địa điểm", hint: "the place"},
	},
	"giay-gioi-thieu": {
		{key: "person", label: "Họ tên người được giới thiệu", hint: "full name of the person introduced", stated: true},
		{key: "position", label: "Chức vụ, đơn vị công tác của người được giới thiệu", hint: "their position (and unit)"},
		{key: "destination", label: "Cơ quan, tổ chức được cử đến", hint: "the agency they are sent to"},
		{key: "purpose", label: "Về việc (nội dung công việc)", hint: "what they are sent for, without \"Về việc\""},
		{key: "valid_until", label: "Giấy có giá trị đến hết ngày nào", hint: "the last day the paper is valid, as the user wrote it"},
	},
	"giay-nghi-phep": {
		{key: "request_date", label: "Ngày của đơn đề nghị nghỉ phép", hint: "the date of the leave application"},
		{key: "person", label: "Họ tên người được nghỉ phép", hint: "full name of the person on leave", stated: true},
		{key: "position", label: "Chức vụ, đơn vị công tác của người được nghỉ phép", hint: "their position (and unit)"},
		{key: "duration", label: "Số ngày nghỉ", hint: "how long, e.g. \"05 ngày\"", optional: true},
		{key: "from", label: "Nghỉ từ ngày nào", hint: "first day of leave"},
		{key: "to", label: "Nghỉ đến hết ngày nào", hint: "last day of leave"},
		{key: "place", label: "Nơi nghỉ phép", hint: "where the leave is spent"},
		{key: "counted_as", label: "Số ngày nghỉ được tính vào (nghỉ phép năm …, nghỉ việc riêng, nghỉ không lương)", hint: "what the days count as, e.g. \"nghỉ phép năm 2026\""},
	},
	"bien-ban": {
		{key: "start_time", label: "Thời gian bắt đầu", hint: "start time and date"},
		{key: "venue", label: "Địa điểm", hint: "the place"},
		{key: "participants", label: "Thành phần tham dự", hint: "who attended"},
		{key: "chair_name", label: "Họ tên người chủ trì (chủ tọa)", hint: "the chair's full name", stated: true},
		{key: "chair_position", label: "Chức vụ người chủ trì", hint: "the chair's position", optional: true},
		{key: "secretary_name", label: "Họ tên thư ký (người ghi biên bản)", hint: "the secretary's full name", stated: true},
		{key: "secretary_position", label: "Chức vụ thư ký", hint: "the secretary's position", optional: true},
		{key: "end_time", label: "Thời gian kết thúc", hint: "end time and date, \"\" when not over yet", optional: true},
	},
	"cong-dien": {
		{key: "sender", label: "Cơ quan hoặc chức danh người đứng đầu gửi điện", hint: "who sends the telegram (agency or head's title, e.g. \"Chủ tịch Ủy ban nhân dân tỉnh\"); \"\" for the agency itself", optional: true, stated: true},
		{key: "receivers", label: "Cơ quan, tổ chức nhận điện", hint: "the agencies the telegram is sent to, separated by \";\""},
	},
}

// formSystemRule extends the system prompt of a form type with its
// "form" object (the other types keep the prompt they were tuned with).
func formSystemRule(templateID string) string {
	if templateForms[templateID] == nil {
		return ""
	}
	return "\n- The JSON also has \"form\": {…} with the facts of the form (keys in the user message), each a short value from the user's words, \"\" when not given. Put those facts there, not in paragraphs."
}

// formOutline is the model's part of a form's JSON.
func formOutline(templateID string) string {
	fields := templateForms[templateID]
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(` Add to the JSON "form": an object with these keys, each a short value taken from the user's words ("" when not given; the system writes the labels and the fixed sentences of the form): `)
	for i, f := range fields {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(f.key + " (" + f.hint + ")")
	}
	b.WriteString(".")
	switch templateID {
	case "giay-moi", "giay-gioi-thieu", "giay-nghi-phep":
		// the form's first sentence names the agency
		b.WriteString(` Add "agency_in_sentence": the issuing agency's name of the unit profile (or the one the user wrote) as a sentence writes it ("Sở Nội vụ", "Ủy ban nhân dân xã An Hòa"): the same words, only the capitals changed.`)
	case "cong-dien":
		b.WriteString(` The system writes the "… điện:" line and the receivers under it.`)
	}
	return b.String()
}

// formValues are the draft's form facts that may be used: trimmed, a name
// only when the user wrote it. missing are the labels of the required ones
// absent.
func formValues(templateID string, d *templateDraft, userText string) (values map[string]string, missing []string) {
	values = map[string]string{}
	for _, f := range templateForms[templateID] {
		v := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(string(d.Form[f.key])), ".;,"))
		if f.stated && v != "" && !said(userText, v) {
			v = ""
		}
		if v == "" {
			if !f.optional {
				missing = append(missing, f.label)
			}
			continue
		}
		values[f.key] = fitRunes(v, templateTextMax)
	}
	return values, missing
}

// blankForm fills the required facts left out with templateBlank (the user
// skipped the questions).
func blankForm(templateID string, values map[string]string) {
	for _, f := range templateForms[templateID] {
		if values[f.key] == "" && !f.optional {
			values[f.key] = templateBlank
		}
	}
}

// formBody writes the body of a form type from its facts: the lines of the
// form, then the model's own paragraphs (a giấy mời's notes, a biên bản's
// proceedings, a công điện's message) where the form has them. The last
// paragraph ends with "./.".
func formBody(templateID string, v map[string]string, agency, subject string, extra []templateParagraph) []templateParagraph {
	line := func(text string) templateParagraph { return templateParagraph{Kind: "body", Text: text} }
	var out []templateParagraph
	switch templateID {
	case "giay-moi":
		out = append(out, line(agency+" trân trọng kính mời: "+v["invitee"]+"."), line("Tới dự "+v["event"]+"."))
		if v["chair"] != "" {
			out = append(out, line("Chủ trì: "+v["chair"]+"."))
		}
		out = append(out, line("Thời gian: "+v["time"]+"."), line("Địa điểm: "+v["venue"]+"."))
		out = append(out, extra...)
	case "giay-gioi-thieu":
		out = append(out,
			line(agency+" trân trọng giới thiệu:"),
			line("Ông (bà): "+v["person"]),
			line("Chức vụ: "+v["position"]),
			line("Được cử đến: "+v["destination"]),
			line("Về việc: "+v["purpose"]),
			line("Đề nghị Quý cơ quan tạo điều kiện để ông (bà) có tên ở trên hoàn thành nhiệm vụ."),
			line("Giấy này có giá trị đến hết ngày "+strings.TrimPrefix(v["valid_until"], "ngày ")),
		)
	case "giay-nghi-phep":
		period := "Được nghỉ phép"
		if v["duration"] != "" {
			period += " trong thời gian " + v["duration"] + ","
		}
		period += " kể từ ngày " + strings.TrimPrefix(v["from"], "ngày ") + " đến hết ngày " + strings.TrimPrefix(v["to"], "ngày ") + " tại " + v["place"] + "."
		out = append(out,
			line("Xét Đơn đề nghị nghỉ phép ngày "+strings.TrimPrefix(v["request_date"], "ngày ")+" của ông (bà) "+v["person"]+","),
			line(agency+" cấp cho:"),
			line("Ông (bà): "+v["person"]),
			line("Chức vụ: "+v["position"]),
			line(period),
			line("Số ngày nghỉ phép nêu trên được tính vào thời gian "+v["counted_as"]),
		)
	case "bien-ban":
		with := func(name, position string) string {
			if position == "" {
				return name
			}
			return name + " - " + position
		}
		out = append(out,
			line("Thời gian bắt đầu: "+v["start_time"]+"."),
			line("Địa điểm: "+v["venue"]+"."),
			line("Thành phần tham dự: "+v["participants"]+"."),
			line("Chủ trì (chủ tọa): "+with(v["chair_name"], v["chair_position"])+"."),
			line("Thư ký (người ghi biên bản): "+with(v["secretary_name"], v["secretary_position"])+"."),
			templateParagraph{Kind: "heading", Text: "Nội dung (theo diễn biến " + meetingKind(subject, "/") + "):"},
		)
		// the model repeats the form's lines in the proceedings
		facts := len(out)
		for _, p := range extra {
			// the closing line is the form's own
			if meetingEndRe.MatchString(fold(p.Text)) {
				continue
			}
			// "./." closes the document, not this paragraph
			p.Text = strings.TrimSuffix(strings.TrimSpace(p.Text), "/.")
			// a repeated fact goes, sentence by sentence: the proceedings
			// may follow it in the same paragraph
			for p.Text != "" && formLineRepeated(p.Text, out[:facts]) {
				_, rest, _ := strings.Cut(p.Text, ". ")
				p.Text = strings.TrimSpace(rest)
			}
			if p.Text = proceedingsLabelRe.ReplaceAllString(p.Text, ""); p.Text != "" {
				p.Text = capitalize(p.Text)
				out = append(out, p)
			}
		}
		end := v["end_time"]
		if end == "" {
			end = templateBlank + " giờ " + templateBlank + ", ngày " + templateBlank + " tháng " + templateBlank + " năm " + templateBlank
		}
		out = append(out, line(capitalize(meetingKind(subject, ", "))+" kết thúc vào "+end))
	case "cong-dien":
		out = append(out, extra...)
	}
	if n := len(out); n > 0 {
		out[n-1].Text = endWith(out[n-1].Text, "./.")
	}
	return out
}

// meetingKind is how a biên bản names its meeting: the one the trích yếu
// names, else all three as the form writes them.
func meetingKind(subject, sep string) string {
	s := fold(subject)
	for _, k := range []string{"hội nghị", "hội thảo", "cuộc họp"} {
		if strings.Contains(s, k) {
			return k
		}
	}
	if strings.Contains(s, "họp") {
		return "cuộc họp"
	}
	if sep == "/" {
		return "cuộc họp/hội nghị/hội thảo"
	}
	return "cuộc họp (hội nghị, hội thảo)"
}

// formLineRepeated: text restates a labelled fact line of the form
// ("Thành phần tham dự: …").
func formLineRepeated(text string, lines []templateParagraph) bool {
	label, _, ok := strings.Cut(fold(text), ":")
	if !ok {
		return false
	}
	for _, l := range lines {
		if have, _, _ := strings.Cut(fold(l.Text), ":"); have == label || strings.HasPrefix(have, label+" (") {
			return true
		}
	}
	return false
}

// proceedingsLabelRe is the model's own label before the proceedings
// ("Nội dung: …", "Nội dung cuộc họp: …"), which the heading already says.
var proceedingsLabelRe = regexp.MustCompile(`(?i)^nội dung[^:]{0,40}:\s*`)

// meetingEndRe is the model's own "Cuộc họp kết thúc vào …" line.
var meetingEndRe = regexp.MustCompile(`^(cuộc họp|hội nghị|hội thảo|buổi (họp|làm việc))[^.]{0,40}kết thúc`)

// expandAgencyAbbr is the folded agency name with UBND / HĐND spelled
// out, to compare a collective with the agency it belongs to.
func expandAgencyAbbr(s string) string {
	s = " " + fold(s) + " "
	for abbr, full := range map[string]string{"ubnd": "ủy ban nhân dân", "hđnd": "hội đồng nhân dân"} {
		s = strings.ReplaceAll(s, " "+abbr+" ", " "+full+" ")
	}
	return strings.ReplaceAll(strings.TrimSpace(s), "uỷ", "ủy")
}

func capitalize(s string) string {
	for i, r := range s {
		return strings.ToUpper(string(r)) + s[i+len(string(r)):]
	}
	return s
}

// telegramReceivers are the "- …;" lines under "… điện:".
func telegramReceivers(receivers string) []string {
	var out []string
	for _, r := range strings.FieldsFunc(receivers, func(c rune) bool { return c == ';' || c == '\n' }) {
		if r = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(r), "-–•+ ")); r != "" {
			out = append(out, fitRunes("- "+endWith(r, ";"), templateTextMax))
		}
		if len(out) == templateRecipientsMax {
			break
		}
	}
	return out
}

// agencyInSentence is the agency as a sentence of the form names it
// ("Sở Nội vụ trân trọng kính mời"): the model's casing of the saved name
// when it is that name and nothing more, else the saved name.
func agencyInSentence(agency, model string) string {
	model = strings.Join(strings.Fields(model), " ")
	if model != "" && strings.EqualFold(model, agency) && model != strings.ToUpper(model) {
		return model
	}
	if agency == strings.ToUpper(agency) {
		return sentenceCaseAgency(agency)
	}
	return agency
}

// agencyKinds are the words naming what kind of body an agency is, longest
// first; placeKinds the administrative units before a place name.
var (
	agencyKinds = []string{"ủy ban nhân dân", "uỷ ban nhân dân", "hội đồng nhân dân", "ủy ban mặt trận tổ quốc việt nam", "văn phòng",
		"công an", "bộ chỉ huy quân sự", "ban chỉ huy quân sự", "trung tâm", "tổng cục", "chi cục", "cục", "vụ", "sở", "bộ",
		"phòng", "ban", "viện", "trường", "bệnh viện", "đội"}
	placeKinds = map[string]bool{"tỉnh": true, "thành": true, "phố": true, "xã": true, "phường": true, "huyện": true,
		"quận": true, "thị": true, "trấn": true, "đặc": true, "khu": true}
)

// sentenceCaseAgency writes an agency name saved in capitals as a sentence
// names it (NĐ30 Phụ lục II): the kind of body with a capital, the
// first word of its own name with a capital, every word of a place name
// with a capital ("SỞ NỘI VỤ" → "Sở Nội vụ", "ỦY BAN NHÂN DÂN XÃ AN HÒA" →
// "Ủy ban nhân dân xã An Hòa"); a known agency the model cased wins
// (agencyInSentence).
func sentenceCaseAgency(agency string) string {
	words := strings.Fields(strings.ToLower(agency))
	if len(words) == 0 {
		return agency
	}
	kind := 1
	for _, k := range agencyKinds {
		if n := len(strings.Fields(k)); n <= len(words) && strings.Join(words[:n], " ") == k {
			kind = n
			break
		}
	}
	words[0] = capitalize(words[0])
	inPlace, nameStarted := false, false
	for i := kind; i < len(words); i++ {
		switch {
		case placeKinds[words[i]]:
			inPlace = true
		case inPlace:
			words[i] = capitalize(words[i])
		case !nameStarted || words[i-1] == "và":
			// each part of a joint name: "Tài nguyên và Môi trường"
			words[i] = capitalize(words[i])
			nameStarted = true
		}
	}
	return strings.Join(words, " ")
}
