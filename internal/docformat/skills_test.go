package docformat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEverySkillIsWellFormed(t *testing.T) {
	names := SkillNames()
	if len(names) < 10 {
		t.Fatalf("skills = %v", names)
	}
	for _, name := range names {
		s, ok := loadSkill(name)
		if !ok || s.Description == "" || strings.HasPrefix(s.Body, "---") || !strings.Contains(s.Body, "# ") {
			t.Errorf("skill %s: frontmatter/body malformed (desc=%q)", name, s.Description)
		}
		if name != GeneralSkill && !strings.Contains(s.Body, GeneralSkill) {
			t.Errorf("skill %s should point to %s", name, GeneralSkill)
		}
	}
	// every type with its own rule set has its own skill
	for _, typ := range []string{"cong_van", "quyet_dinh", "nghi_quyet", "to_trinh", "bao_cao",
		"ke_hoach", "thong_bao", "bien_ban", "chi_thi"} {
		if _, ok := loadSkill(TypeSkillName(typ)); !ok {
			t.Errorf("no skill for %s", typ)
		}
	}
}

func TestSkillsForType(t *testing.T) {
	got := SkillsFor("cong_van")
	if len(got) != 2 || got[0].Name != GeneralSkill || got[1].Name != "the-thuc-cong-van" {
		t.Fatalf("cong_van skills = %+v", got)
	}
	for _, typ := range []string{"unknown", "", "giay_moi"} {
		if s := SkillsFor(typ); len(s) != 1 || s[0].Name != GeneralSkill {
			t.Errorf("%q → %d skills", typ, len(s))
		}
	}
}

func TestExtractFormatAndRenderData(t *testing.T) {
	content, _ := fixture(t, "fx_ky_thay_missing_kt")
	r := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic, SourceName: "cv.docx"})
	var signer *ComponentFormat
	for i := range r.Format {
		if r.Format[i].Key == "chuc_danh" {
			signer = &r.Format[i]
		}
	}
	if signer == nil || len(signer.Lines) != 2 || signer.Lines[0].Text != "TRƯỞNG PHÒNG" ||
		!signer.Lines[1].Bold || signer.Lines[1].Zone != ZoneRight {
		t.Fatalf("chuc_danh format = %+v", signer)
	}
	data := RenderData(r)
	for _, want := range []string{"# Dữ liệu thể thức", "## Chức vụ người ký (chuc_danh)",
		`"PHÓ TRƯỞNG PHÒNG" [`, "## Nơi nhận (noi_nhan)", `"- Lưu: PA05 (Đ4)."`, "Trang: "} {
		if !strings.Contains(data, want) {
			t.Errorf("data lacks %q", want)
		}
	}
	if strings.Contains(data, "<skill") {
		t.Error("RenderData must not carry the skills")
	}
	if got := r.Skills; len(got) != 2 || got[1] != "the-thuc-cong-van" {
		t.Errorf("report skills = %v", got)
	}
	agent := RenderForAgent(r)
	if !strings.Contains(agent, `<skill name="the-thuc-chung">`) || !strings.Contains(agent, `<skill name="the-thuc-cong-van">`) {
		t.Error("fallback rendering must carry both skills")
	}
}

func TestBodyKeepsOpeningAndClosing(t *testing.T) {
	content, _ := fixture(t, "fx_bao_cao_two_tables")
	l := InspectDocx(content)
	seg := Segment(l)
	// pad the body so it exceeds the cap
	for i := 0; i < 20; i++ {
		seg.add("noi_dung", l.Paragraphs[len(l.Paragraphs)-1].Index, "", ZoneFull, 1000+i)
	}
	body := seg.Components["noi_dung"]
	last := body.Paras[len(body.Paras)-1]
	for i := 0; i < 20; i++ {
		body.Paras = append(body.Paras, last)
		body.Zones = append(body.Zones, ZoneFull)
	}
	for _, cf := range ExtractFormat(l, seg) {
		if cf.Key != "noi_dung" {
			continue
		}
		if len(cf.Lines) != maxBodyLines || cf.Omitted == 0 {
			t.Fatalf("body lines=%d omitted=%d", len(cf.Lines), cf.Omitted)
		}
		return
	}
	t.Fatal("no body format")
}

type evalSpy struct {
	got   []Message
	reply string
	err   error
}

func (e *evalSpy) Complete(_ context.Context, msgs []Message) (string, error) {
	e.got = msgs
	return e.reply, e.err
}

func TestEvaluateWithSkills(t *testing.T) {
	content, _ := fixture(t, "fx_ky_thay_missing_kt")
	r := Check(context.Background(), content, Options{Segmenter: SegmenterHeuristic})
	spy := &evalSpy{reply: "<think>dòng trên không có KT.</think>\n## Kết luận\nChưa đạt."}
	text, err := EvaluateWithSkills(context.Background(), spy, r)
	if err != nil || text != "## Kết luận\nChưa đạt." {
		t.Fatalf("text=%q err=%v", text, err)
	}
	sys, user := spy.got[0], spy.got[1]
	if sys.Role != "system" || !strings.Contains(sys.Content, `<skill name="the-thuc-chung">`) ||
		!strings.Contains(sys.Content, `<skill name="the-thuc-cong-van">`) {
		t.Error("skills must be the system turn")
	}
	if !strings.Contains(user.Content, "PHÓ TRƯỞNG PHÒNG") || strings.Contains(user.Content, "<skill") {
		t.Error("the user turn carries the data only")
	}

	if _, err := EvaluateWithSkills(context.Background(), &evalSpy{err: errors.New("down")}, r); err == nil {
		t.Error("model errors must surface")
	}
	if _, err := EvaluateWithSkills(context.Background(), &evalSpy{reply: "<think>…</think>  "}, r); err == nil {
		t.Error("an empty evaluation is an error")
	}
}
