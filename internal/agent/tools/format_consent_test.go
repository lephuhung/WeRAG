package tools

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFormatFixConsent(t *testing.T) {
	// "fix it now": no plan needs to have been shown
	immediate := []string{"sửa luôn giúp tôi", "Áp dụng ngay", "tự sửa giúp tôi luôn", ""}
	// answers to a plan shown in an earlier turn
	replies := []string{"Đồng ý, áp dụng các sửa đó", "OK", "được, tiến hành đi", "Được.", "ừ", "đồng ý sửa", "Áp dụng các sửa thể thức này"}
	// never consent
	no := []string{
		"Sửa các lỗi thể thức đó giúp tôi", "Kiểm tra thể thức văn bản này", "không áp dụng",
		"chưa đồng ý, xem lại đã", "văn bản này sai gì?",
		"Kiểm tra thể thức văn bản được ban hành hôm qua",
		"Văn bản này có áp dụng đúng Nghị định 30 không",
		"Kiểm tra lại rồi áp dụng giúp tôi",
		"Văn bản này được ký bởi ai, ok không",
		"Trích yếu được viết như thế nào thì đúng chuẩn",
	}
	for _, q := range immediate {
		if !formatFixConsent(q, false) || !formatFixConsent(q, true) {
			t.Errorf("expected consent for %q with or without a plan", q)
		}
	}
	for _, q := range replies {
		if !formatFixConsent(q, true) {
			t.Errorf("expected consent for %q after a plan", q)
		}
		if formatFixConsent(q, false) {
			t.Errorf("%q is no consent while no plan was shown", q)
		}
	}
	for _, q := range no {
		if formatFixConsent(q, true) || formatFixConsent(q, false) {
			t.Errorf("expected no consent for %q", q)
		}
	}
}

func TestFormatPlanShownOnlyCountsEarlierTurns(t *testing.T) {
	turn1 := types.WithTurnID(context.Background(), "msg-1")
	turn2 := types.WithTurnID(context.Background(), "msg-2")
	if formatPlanShownEarlier(turn2, "s-plan", "doc") {
		t.Fatal("no plan was shown")
	}
	rememberFormatPlan(turn1, "s-plan", "doc")
	if formatPlanShownEarlier(turn1, "s-plan", "doc") {
		t.Error("the plan shown in this turn is not one the user answered")
	}
	if !formatPlanShownEarlier(turn2, "s-plan", "doc") {
		t.Error("the next turn answers the plan")
	}
	if formatPlanShownEarlier(turn2, "s-plan", "other-doc") || formatPlanShownEarlier(turn2, "other-session", "doc") {
		t.Error("a plan belongs to its session and document")
	}
	forgetFormatPlan("s-plan", "doc")
	if formatPlanShownEarlier(turn2, "s-plan", "doc") {
		t.Error("an applied plan needs a new review")
	}
}
