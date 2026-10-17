package service

import (
	"strings"
	"testing"
)

func TestLinkifyContent_BasicCJK(t *testing.T) {
	refs := []linkRef{
		{slug: "beijing", matchText: "北京"},
	}
	got, changed := linkifyContent("我住在北京市", refs, "")
	if !changed {
		t.Fatalf("expected change")
	}
	want := "我住在[[beijing|北京]]市"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLinkifyContent_LongerNameWinsOverSubstring(t *testing.T) {
	refs := []linkRef{
		{slug: "beijing", matchText: "北京"},
		{slug: "bupt", matchText: "北京邮电大学"},
	}
	got, changed := linkifyContent("我就读于北京邮电大学", refs, "")
	if !changed {
		t.Fatalf("expected change")
	}
	// The longer match should win; "北京" must not swallow the prefix.
	if !strings.Contains(got, "[[bupt|北京邮电大学]]") {
		t.Fatalf("longer match not preferred: %q", got)
	}
	if strings.Contains(got, "[[beijing|") {
		t.Fatalf("shorter substring should not have linked: %q", got)
	}
}

func TestLinkifyContent_ASCIIWordBoundary(t *testing.T) {
	refs := []linkRef{
		{slug: "ai", matchText: "AI"},
	}
	// Should NOT match "AI" inside "TRAINING" or "PAINT".
	in := "TRAINING and PAINT are words."
	got, changed := linkifyContent(in, refs, "")
	if changed {
		t.Fatalf("should not change, got %q", got)
	}

	// SHOULD match standalone "AI".
	in2 := "AI is cool."
	got2, changed2 := linkifyContent(in2, refs, "")
	if !changed2 {
		t.Fatalf("expected change, got %q", got2)
	}
	if !strings.HasPrefix(got2, "[[ai|AI]] ") {
		t.Fatalf("standalone AI should link: %q", got2)
	}
}

func TestLinkifyContent_SkipsExistingWikiLink(t *testing.T) {
	refs := []linkRef{
		{slug: "beijing", matchText: "北京"},
	}
	// Already linked — should not double-wrap.
	in := "我住在[[beijing|北京]]市"
	got, changed := linkifyContent(in, refs, "")
	if changed {
		t.Fatalf("should not change already-linked content: %q", got)
	}
}

func TestLinkifyContent_SkipsInsideFencedCode(t *testing.T) {
	refs := []linkRef{
		{slug: "go", matchText: "Go"},
	}
	in := "Prose mentions Go.\n\n```go\nfunc Go() {}\n```\nAfter code."
	got, _ := linkifyContent(in, refs, "")
	// The prose occurrence should link.
	if !strings.Contains(got, "Prose mentions [[go|Go]].") {
		t.Fatalf("prose occurrence not linked: %q", got)
	}
	// The occurrence inside the fenced block must be untouched. Since linkifyContent
	// only wraps the first eligible match, the code-block one is skipped anyway,
	// but double-check that no link was injected inside the code fence.
	if strings.Contains(got, "func [[go|Go]]") {
		t.Fatalf("should not link inside fenced code: %q", got)
	}
}

func TestLinkifyContent_SkipsInsideInlineCode(t *testing.T) {
	refs := []linkRef{
		{slug: "foo", matchText: "Foo"},
	}
	// Only occurrence is inside `Foo` — must NOT be linked.
	in := "Use the `Foo` type."
	got, changed := linkifyContent(in, refs, "")
	if changed {
		t.Fatalf("should not link inside inline code: %q", got)
	}

	// Second case: prose before the inline code — should link the prose one.
	in2 := "Foo is a type. Use `Foo` here."
	got2, changed2 := linkifyContent(in2, refs, "")
	if !changed2 {
		t.Fatalf("expected change, got %q", got2)
	}
	if !strings.HasPrefix(got2, "[[foo|Foo]] is") {
		t.Fatalf("prose occurrence not linked: %q", got2)
	}
	if strings.Contains(got2, "`[[foo|Foo]]`") {
		t.Fatalf("inline code occurrence should not be linked: %q", got2)
	}
}

func TestLinkifyContent_SkipsInsideMarkdownLink(t *testing.T) {
	refs := []linkRef{
		{slug: "hanoi", matchText: "Hà Nội"},
	}
	// Occurrence inside [text](url) should be skipped.
	in := "Xem [Hà Nội](https://example.com/hanoi)"
	got, changed := linkifyContent(in, refs, "")
	if changed {
		t.Fatalf("should not link inside markdown link text: %q", got)
	}
}

func TestLinkifyContent_OnlyFirstOccurrenceWrapped(t *testing.T) {
	refs := []linkRef{
		{slug: "hanoi", matchText: "Hà Nội"},
	}
	in := "Hà Nội rất lớn. Hà Nội có nhiều người. Hà Nội là thủ đô."
	got, changed := linkifyContent(in, refs, "")
	if !changed {
		t.Fatalf("expected change")
	}
	// Only first match should be wrapped.
	count := strings.Count(got, "[[hanoi|Hà Nội]]")
	if count != 1 {
		t.Fatalf("expected exactly 1 link, got %d: %q", count, got)
	}
	// Later occurrences remain bare.
	if strings.Count(got, "Hà Nội") != 3 {
		t.Fatalf("expected 3 total Hà Nội (incl. one inside the link), got content %q", got)
	}
}

func TestLinkifyContent_SkipsSelfSlug(t *testing.T) {
	refs := []linkRef{
		{slug: "hanoi", matchText: "Hà Nội"},
	}
	// Rendering on the beijing page itself — must not self-link.
	got, changed := linkifyContent("Hà Nội là thủ đô", refs, "hanoi")
	if changed {
		t.Fatalf("should not self-link: %q", got)
	}
}

func TestLinkifyContent_SkipsWhenSlugAlreadyUsed(t *testing.T) {
	// Title matches by alias, but slug already appears via a different mention.
	refs := []linkRef{
		{slug: "hanoi", matchText: "Hà Nội"},
		{slug: "hanoi", matchText: "Thủ đô"},
	}
	in := "Tôi đi công tác ở [[hanoi|Thủ đô]], sau đó đến Hà Nội."
	got, changed := linkifyContent(in, refs, "")
	if changed {
		t.Fatalf("should skip when slug already linked: %q", got)
	}
}

func TestLinkifyContent_MultipleRefsDisjoint(t *testing.T) {
	refs := []linkRef{
		{slug: "hanoi", matchText: "Hà Nội"},
		{slug: "hue", matchText: "Huế"},
	}
	in := "Hôm nay tôi đi Hà Nội, ngày mai đi Huế."
	got, changed := linkifyContent(in, refs, "")
	if !changed {
		t.Fatalf("expected change")
	}
	if !strings.Contains(got, "[[hanoi|Hà Nội]]") {
		t.Fatalf("missing hanoi link: %q", got)
	}
	if !strings.Contains(got, "[[hue|Huế]]") {
		t.Fatalf("missing hue link: %q", got)
	}
}

func TestLinkifyContent_EmptyOrNoMatch(t *testing.T) {
	if _, c := linkifyContent("", []linkRef{{slug: "x", matchText: "y"}}, ""); c {
		t.Fatalf("empty content must not change")
	}
	if _, c := linkifyContent("hello", nil, ""); c {
		t.Fatalf("nil refs must not change")
	}
	if _, c := linkifyContent("hello world", []linkRef{{slug: "x", matchText: "zzz"}}, ""); c {
		t.Fatalf("no match must not change")
	}
}

func TestFindFirstSafeMatch_BoundaryCases(t *testing.T) {
	// Case where the first occurrence is unsafe (in code), second is safe.
	s := "See `AI` in docs. AI rocks."
	forb, _ := computeForbiddenSpans(s)
	idx := findFirstSafeMatch(s, "AI", forb)
	// First AI is inside `...`, second is safe.
	want := strings.Index(s, "AI rocks")
	if idx != want {
		t.Fatalf("expected safe match at %d, got %d", want, idx)
	}
}

func TestComputeForbiddenSpans_FencedCode(t *testing.T) {
	s := "before\n```\nhidden\n```\nafter"
	spans, _ := computeForbiddenSpans(s)
	if len(spans) == 0 {
		t.Fatalf("expected at least one span")
	}
	// Ensure "hidden" is inside a forbidden span.
	h := strings.Index(s, "hidden")
	if !spanContains(spans, h, h+len("hidden")) {
		t.Fatalf("hidden should be forbidden, spans=%v", spans)
	}
	// "before" and "after" must NOT be forbidden.
	b := strings.Index(s, "before")
	if spanContains(spans, b, b+len("before")) {
		t.Fatalf("before should not be forbidden")
	}
	a := strings.Index(s, "after")
	if spanContains(spans, a, a+len("after")) {
		t.Fatalf("after should not be forbidden")
	}
}

func TestComputeForbiddenSpans_CollectsUsedSlugs(t *testing.T) {
	s := "Refer to [[hanoi|Hà Nội]] and [[hue]] and [[nope|x]]."
	_, used := computeForbiddenSpans(s)
	for _, slug := range []string{"hanoi", "hue", "nope"} {
		if _, ok := used[slug]; !ok {
			t.Fatalf("expected slug %q in used set, got %v", slug, used)
		}
	}
}

func TestLinkifyContent_SkipsInsideReferenceStyleLink(t *testing.T) {
	refs := []linkRef{
		{slug: "hanoi", matchText: "Hà Nội"},
	}
	// [text][label] form — occurrence should not be linkified.
	in := "See [Hà Nội][capital] for details.\n\n[capital]: https://example.com/hanoi"
	got, changed := linkifyContent(in, refs, "")
	if changed {
		t.Fatalf("should not link inside reference-style link or definition: %q", got)
	}
}

func TestLinkifyContent_SkipsReferenceDefinitionLine(t *testing.T) {
	refs := []linkRef{
		{slug: "example", matchText: "example"},
	}
	// The `example` inside the definition URL must not be wrapped.
	in := "[cap]: https://example.com/x"
	got, changed := linkifyContent(in, refs, "")
	if changed {
		t.Fatalf("should not link inside reference definition: %q", got)
	}
}

func TestLinkifyContent_RepeatedLinkifyIsIdempotent(t *testing.T) {
	refs := []linkRef{
		{slug: "hanoi", matchText: "Hà Nội"},
		{slug: "hue", matchText: "Huế"},
	}
	in := "Hôm nay đi Hà Nội, ngày mai đi Huế."
	once, _ := linkifyContent(in, refs, "")
	twice, changed := linkifyContent(once, refs, "")
	if changed {
		t.Fatalf("second run should be a no-op, got %q", twice)
	}
	if once != twice {
		t.Fatalf("second run altered content:\nbefore: %q\nafter:  %q", once, twice)
	}
}
