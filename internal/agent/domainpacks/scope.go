package domainpacks

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// scopeFile lists what a pack does not cover (exclusions → see a lawyer)
// and what the system refuses to help with (refusals). It is optional and
// never served to a model: the matching runs on the user's question in code
// and the notice is code-appended (doc §6), like the draft notice.
const scopeFile = "scope.yaml"

// ScopeNoticeMarker opens the code-appended scope notice; StripFooter cuts
// it from history.
const ScopeNoticeMarker = "\n\n---\n**Lưu ý về phạm vi**\n"

// minScopePhraseRunes is the shortest phrase accepted.
const minScopePhraseRunes = 4

// Scope is scope.yaml.
type Scope struct {
	Exclusions []ScopeRule `yaml:"exclusions"`
	Refusals   []ScopeRule `yaml:"refusals"`
}

// ScopeRule is one topic: a short name used in the notice and the phrases
// of a question that mean it. Alternative (refusals only) is the lawful
// course the notice offers instead.
type ScopeRule struct {
	Name        string   `yaml:"name"`
	Phrases     []string `yaml:"phrases"`
	Alternative string   `yaml:"alternative"`
}

// parseScope decodes scope.yaml strictly and checks it: names, phrases of at
// least 4 characters unique in the pack, an alternative for each refusal,
// no URL.
func parseScope(data []byte) (*Scope, []error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(scopeFile+": "+format, args...))
	}
	var s Scope
	if err := decodeStrict(data, &s); err != nil {
		fail("%v", err)
		return nil, errs
	}
	if len(s.Exclusions)+len(s.Refusals) == 0 {
		fail("exclusions and refusals are both empty")
	}
	seen := map[string]string{}
	check := func(kind string, i int, r ScopeRule, needAlternative bool) {
		at := fmt.Sprintf("%s[%d] (%s)", kind, i, r.Name)
		if strings.TrimSpace(r.Name) == "" {
			fail("%s: name is required", at)
		}
		if needAlternative && strings.TrimSpace(r.Alternative) == "" {
			fail("%s: alternative is required for a refusal", at)
		}
		if !needAlternative && strings.TrimSpace(r.Alternative) != "" {
			fail("%s: alternative is only for refusals", at)
		}
		if len(r.Phrases) == 0 {
			fail("%s: phrases are required", at)
		}
		for j, p := range r.Phrases {
			key := normText(strings.TrimSpace(p))
			switch {
			case utf8.RuneCountInString(key) < minScopePhraseRunes:
				fail("%s: phrases[%d] %q is shorter than %d characters", at, j, p, minScopePhraseRunes)
			case seen[key] != "":
				fail("%s: phrase %q is also in %s", at, p, seen[key])
			}
			seen[key] = at
		}
		for _, text := range []string{r.Name, r.Alternative} {
			if urlPattern.MatchString(text) {
				fail("%s: contains a URL", at)
			}
		}
	}
	for i, r := range s.Exclusions {
		check("exclusions", i, r, false)
	}
	for i, r := range s.Refusals {
		check("refusals", i, r, true)
	}
	return &s, errs
}

// Scope is the pack's scope.yaml, nil when it has none.
func (p *Pack) Scope() *Scope {
	return p.scope
}

// matches reports whether question (already normalised) holds one of the
// rule's phrases on word boundaries.
func (r ScopeRule) matches(question string) bool {
	for _, p := range r.Phrases {
		if containsPhrase(question, normText(strings.TrimSpace(p))) {
			return true
		}
	}
	return false
}

// ScopeNotice is the block appended after an answer whose question touches
// a pack's exclusions or refusals: one line for the exclusions matched,
// one per refusal matched. "" when nothing matches. The match is on the
// user's question only, never on the model's text.
func (s *Source) ScopeNotice(question string) string {
	q := normText(question)
	if strings.TrimSpace(q) == "" {
		return ""
	}
	var excluded []string
	var lines []string
	seen := map[string]bool{}
	for _, pack := range s.Packs() {
		scope := pack.Scope()
		if scope == nil {
			continue
		}
		for _, r := range scope.Exclusions {
			if r.matches(q) && !seen["x:"+r.Name] {
				seen["x:"+r.Name] = true
				excluded = append(excluded, strings.TrimSpace(r.Name))
			}
		}
		for _, r := range scope.Refusals {
			if r.matches(q) && !seen["r:"+r.Name] {
				seen["r:"+r.Name] = true
				lines = append(lines, fmt.Sprintf("- Hệ thống không hỗ trợ %s; hành vi này có thể bị xử lý hành chính "+
					"hoặc hình sự. Phương án hợp pháp: %s. Nên hỏi luật sư hoặc kế toán hành nghề.",
					strings.TrimSpace(r.Name), strings.TrimSuffix(strings.TrimSpace(r.Alternative), ".")))
			}
		}
	}
	if len(excluded) > 0 {
		lines = append([]string{fmt.Sprintf("- Nội dung này nằm ngoài phạm vi cẩm nang (%s); hãy hỏi luật sư hoặc "+
			"người có chứng chỉ hành nghề trước khi làm hồ sơ.", strings.Join(excluded, ", "))}, lines...)
	}
	if len(lines) == 0 {
		return ""
	}
	return ScopeNoticeMarker + strings.Join(lines, "\n") + "\n"
}
