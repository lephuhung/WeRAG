package docparser

import (
	"strings"
	"testing"
)

func TestImageOriginalRefsMatchesPathVariants(t *testing.T) {
	md := strings.Join([]string{
		"![](images/cover.jpg)",
		`<img src="./images/cover.jpg"/>`,
		"![](plain.jpg)",
	}, "\n")

	got := mineruImageOriginalRefs(md, "cover.jpg")
	if len(got) != 2 || got[0] != "images/cover.jpg" || got[1] != "./images/cover.jpg" {
		t.Fatalf("refs for cover.jpg = %q", got)
	}
	if got := mineruImageOriginalRefs(md, "plain.jpg"); len(got) != 1 {
		t.Fatalf("refs for plain.jpg = %q", got)
	}
}

// A file name with spaces ("images/trang 1.jpg") must still be matched: the
// markdown regex allows whitespace inside the URL group.
func TestImageOriginalRefsMatchesPathsWithSpaces(t *testing.T) {
	got := mineruImageOriginalRefs("![](images/trang 1.jpg)", "trang 1.jpg")
	if len(got) != 1 || got[0] != "images/trang 1.jpg" {
		t.Fatalf("refs = %q", got)
	}
}

func TestUnescapeMarkdownImageSyntax(t *testing.T) {
	if got := unescapeMarkdownImageSyntax(`!\[alt\](images/a.png)`); got != "![alt](images/a.png)" {
		t.Fatalf("got %q", got)
	}
}
