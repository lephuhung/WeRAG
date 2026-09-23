package people

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type LookupRequest struct {
	LookupType       string
	Query            string
	Limit            int64
	ContinuePipeline bool
}

var lookupNamePattern = regexp.MustCompile(`(?i)^\s*(?:tra\s+cứu|tra\s+cuu|tìm\s+thông\s+tin|tim\s+thong\s+tin|tìm\s+người|tim\s+nguoi|lookup|cho\s+biết\s+thông\s+tin|cho\s+biet\s+thong\s+tin)\s+(?:về\s+|ve\s+)?(?:người\s+|nguoi\s+)?(?:tên\s+|ten\s+)?([\p{L}][\p{L}\s.'-]{1,80})\s*[?!]*$`)

var lookupIntentTerms = []string{
	"tra cuu",
	"tim thong tin",
	"tim nguoi",
	"kiem tra thong tin",
	"lookup",
	"cua ai",
	"ai so huu",
	"chu so huu",
	"nguoi nao",
	"ho so cua",
	"cho biet thong tin",
}

var phoneTerms = []string{
	"so dien thoai",
	"dien thoai",
	"sdt",
	"phone",
	"mobile",
}

var cccdTerms = []string{
	"cccd",
	"cmnd",
	"can cuoc",
	"chung minh nhan dan",
	"national id",
}

var bhxhTerms = []string{
	"bhxh",
	"bao hiem xa hoi",
	"social insurance",
}

var secondaryIntentTerms = []string{
	"doi chieu",
	"quy dinh",
	"van ban",
	"nghi dinh",
	"thong tu",
	"ho so",
	"vi pham",
	"tuan thu",
	"danh gia",
	"so sanh",
	"tai lieu",
	"phap luat",
}

func foldLookupText(s string) string {
	s = strings.ToLower(s)
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == 'đ' {
			r = 'd'
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func containsLookupTerm(folded string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(folded, term) {
			return true
		}
	}
	return false
}

func isStandaloneLookupNumber(s string) bool {
	hasDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == ' ' || r == '\t' || r == '.' || r == '-' || r == '(' || r == ')' || r == '+':
		default:
			return false
		}
	}
	return hasDigit
}

func detectLookupName(raw string, continuePipeline bool) (LookupRequest, bool) {
	m := lookupNamePattern.FindStringSubmatch(raw)
	if m == nil {
		return LookupRequest{}, false
	}
	name := strings.Trim(m[1], " \t\r\n.,!?;:")
	if len(strings.Fields(name)) < 2 {
		return LookupRequest{}, false
	}
	for _, r := range name {
		if r >= '0' && r <= '9' {
			return LookupRequest{}, false
		}
	}
	foldedName := foldLookupText(name)
	if containsLookupTerm(foldedName, phoneTerms) ||
		containsLookupTerm(foldedName, cccdTerms) ||
		containsLookupTerm(foldedName, bhxhTerms) ||
		strings.Contains(foldedName, "chu so huu") {
		return LookupRequest{}, false
	}
	return LookupRequest{LookupType: "name", Query: name, Limit: 10, ContinuePipeline: continuePipeline}, true
}

func DetectLookupRequest(text string) (LookupRequest, bool) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return LookupRequest{}, false
	}
	folded := foldLookupText(raw)
	hasLookupIntent := containsLookupTerm(folded, lookupIntentTerms)
	hasBHXH := containsLookupTerm(folded, bhxhTerms)
	hasPhone := containsLookupTerm(folded, phoneTerms)
	hasCCCD := containsLookupTerm(folded, cccdTerms)
	standalone := isStandaloneLookupNumber(raw)
	continuePipeline := containsLookupTerm(folded, secondaryIntentTerms)

	if hasBHXH {
		if values := extractNumbers(raw, nil, 5); len(values) > 0 {
			return LookupRequest{LookupType: "bhxh", Query: strings.Join(values, ","), ContinuePipeline: continuePipeline}, true
		}
	}
	if hasPhone && (hasLookupIntent || standalone) {
		if values := extractNumbers(raw, []int{10}, 0); len(values) > 0 {
			return LookupRequest{LookupType: "phone", Query: strings.Join(values, ","), ContinuePipeline: continuePipeline}, true
		}
	}
	if hasCCCD {
		if values := extractNumbers(raw, []int{9, 12}, 0); len(values) > 0 {
			return LookupRequest{LookupType: "cccd", Query: strings.Join(values, ","), ContinuePipeline: continuePipeline}, true
		}
	}
	if hasLookupIntent || standalone {
		if values := extractNumbers(raw, []int{10}, 0); len(values) > 0 {
			return LookupRequest{LookupType: "phone", Query: strings.Join(values, ","), ContinuePipeline: continuePipeline}, true
		}
		if values := extractNumbers(raw, []int{9, 12}, 0); len(values) > 0 {
			return LookupRequest{LookupType: "cccd", Query: strings.Join(values, ","), ContinuePipeline: continuePipeline}, true
		}
	}
	if hasLookupIntent {
		return detectLookupName(raw, continuePipeline)
	}
	return LookupRequest{}, false
}
