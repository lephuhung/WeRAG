package service

import "testing"

func TestTemplateMemoryStatementsReadBack(t *testing.T) {
	m := templateUnitRe.FindStringSubmatch("Phòng/bộ phận soạn thảo văn bản: Phòng Tổ chức cán bộ; viết tắt: TCCB")
	if m == nil || m[1] != "Phòng Tổ chức cán bộ" || m[2] != "TCCB" {
		t.Fatalf("unit: %q", m)
	}
	// edited by hand in the memory manager, without the abbreviation
	if m := templateUnitRe.FindStringSubmatch("Phòng/bộ phận soạn thảo văn bản: Văn phòng"); m == nil || m[1] != "Văn phòng" || m[2] != "" {
		t.Fatalf("unit without abbreviation: %q", m)
	}
	if m := templatePositionRe.FindStringSubmatch("Chức vụ: Chuyên viên chính."); m == nil || m[1] != "Chuyên viên chính" {
		t.Fatalf("position: %q", m)
	}
}
