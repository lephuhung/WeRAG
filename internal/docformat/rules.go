package docformat

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Rule engine for the thể-thức checks. Rule sets are JSON under rules/: a
// shared _base.json (NĐ30/2020 Phụ lục I: khổ giấy, lề, font, cỡ chữ, căn
// lề) plus one file per document type that inherits and overrides it — a
// type check with the same id replaces the base one.
//
// Check kinds: prop (a paragraph property over a component's paragraphs;
// mode all | majority (≥70%) | any), page_setup (a section property) and
// stray_chars (a symbol typed inside a word in any paragraph).
// Rules that need judgment — signing authority, Nơi nhận, wording — are
// not coded here: the agent evaluates them with the document-type skills
// (skills/), on the format data this package extracts.
// Operators: eq, ne, in, range [min,max], regex, ge, le.

//go:embed rules/*.json
var rulesFS embed.FS

// RuleCheck is one rule as written in the JSON files.
type RuleCheck struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Component string `json:"component"`
	Prop      string `json:"prop"`
	Op        string `json:"op"`
	Value     any    `json:"value"`
	Mode      string `json:"mode"`
	Desc      string `json:"desc"`
	Severity  string `json:"severity"`
}

// RuleSet is a document type's merged rules.
type RuleSet struct {
	Type               string      `json:"type"`
	Label              string      `json:"label"`
	RequiredComponents []string    `json:"required_components"`
	OptionalComponents []string    `json:"optional_components"`
	Order              []string    `json:"order"`
	Checks             []RuleCheck `json:"checks"`
}

// CheckByID returns the rule with the given id, nil when the set has none.
func (rs *RuleSet) CheckByID(id string) *RuleCheck {
	if rs == nil {
		return nil
	}
	for i := range rs.Checks {
		if rs.Checks[i].ID == id {
			c := rs.Checks[i]
			return &c
		}
	}
	return nil
}

// ParaSatisfies reports whether paragraph p, taken as a whole, satisfies the
// prop rule c with the same comparison Evaluate uses. The sides of a
// tab-split paragraph are not considered.
func ParaSatisfies(p *Para, c RuleCheck) (bool, error) {
	if p == nil {
		return false, fmt.Errorf("no paragraph")
	}
	actual := paraAttr(p, c.Prop)
	if c.Prop == "font_name" && actual == nil {
		return false, nil
	}
	return compare(c.Op, actual, c.Value)
}

// AvailableTypes lists the document types with their own rule set.
func AvailableTypes() []string {
	entries, err := rulesFS.ReadDir("rules")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasSuffix(n, ".json") && !strings.HasPrefix(n, "_") {
			out = append(out, strings.TrimSuffix(n, ".json"))
		}
	}
	sort.Strings(out)
	return out
}

func loadRuleFile(name string) (map[string]json.RawMessage, error) {
	data, err := rulesFS.ReadFile("rules/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("unknown document type rule set %q", name)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("rule set %q: %w", name, err)
	}
	return raw, nil
}

// LoadRuleSet returns the rule set of docType merged over _base ("base"
// returns _base alone).
func LoadRuleSet(docType string) (*RuleSet, error) {
	baseRaw, err := loadRuleFile("_base")
	if err != nil {
		return nil, err
	}
	merged := map[string]json.RawMessage{}
	for k, v := range baseRaw {
		merged[k] = v
	}
	var base RuleSet
	if err := remarshal(baseRaw, &base); err != nil {
		return nil, err
	}
	if docType == "base" || docType == "_base" || docType == "generic" {
		return &base, nil
	}
	specRaw, err := loadRuleFile(docType)
	if err != nil {
		return nil, err
	}
	for k, v := range specRaw {
		if k != "checks" {
			merged[k] = v
		}
	}
	var spec RuleSet
	if err := remarshal(specRaw, &spec); err != nil {
		return nil, err
	}
	var rs RuleSet
	if err := remarshal(merged, &rs); err != nil {
		return nil, err
	}
	own := map[string]RuleCheck{}
	for _, c := range spec.Checks {
		own[c.ID] = c
	}
	rs.Checks = nil
	for _, c := range base.Checks {
		if o, ok := own[c.ID]; ok {
			rs.Checks = append(rs.Checks, o)
			delete(own, c.ID)
		} else {
			rs.Checks = append(rs.Checks, c)
		}
	}
	for _, c := range spec.Checks {
		if _, ok := own[c.ID]; ok {
			rs.Checks = append(rs.Checks, c)
		}
	}
	rs.Type = docType
	if spec.Type != "" {
		rs.Type = spec.Type
	}
	rs.Label = docType
	if spec.Label != "" {
		rs.Label = spec.Label
	}
	return &rs, nil
}

func remarshal(in map[string]json.RawMessage, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// CheckResult is one evaluated rule.
type CheckResult struct {
	ID        string           `json:"id"`
	Desc      string           `json:"desc"`
	Severity  string           `json:"severity"`
	Component *string          `json:"component"`
	Status    string           `json:"status"` // pass | fail | warn | skip
	Actual    any              `json:"actual"`
	Evidence  []map[string]any `json:"evidence"`
	Note      string           `json:"note,omitempty"`
}

// Check statuses.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusWarn = "warn"
	StatusSkip = "skip"
)

func failStatus(severity string) string {
	if severity == "warn" {
		return StatusWarn
	}
	return StatusFail
}

var spaceRunRe = regexp.MustCompile(`\s+`)

func normFont(s string) *string {
	if s == "" {
		return nil
	}
	v := strings.ToLower(strings.TrimSpace(spaceRunRe.ReplaceAllString(s, " ")))
	return &v
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// pyStr renders a value like Python's str() (regex checks run on it).
func pyStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e16 {
			return strconv.FormatFloat(x, 'f', 1, 64)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case nil:
		return "None"
	}
	return fmt.Sprint(v)
}

func asNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// pyEqual is Python == between JSON-ish values.
func pyEqual(a, b any) bool {
	if an, ok := asNumber(a); ok {
		if bn, ok := asNumber(b); ok {
			return an == bn
		}
		return false
	}
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case nil:
		return b == nil
	}
	return false
}

func compare(op string, actual, expected any) (bool, error) {
	switch op {
	case "eq":
		if as, ok := actual.(string); ok {
			if es, ok := expected.(string); ok {
				return eqPtr(normFont(as), normFont(es)), nil
			}
		}
		return pyEqual(actual, expected), nil
	case "ne":
		ok, err := compare("eq", actual, expected)
		return !ok, err
	case "in":
		list, ok := expected.([]any)
		if !ok {
			return false, fmt.Errorf("'in' needs a list value")
		}
		if as, ok := actual.(string); ok {
			for _, e := range list {
				es, ok := e.(string)
				if !ok {
					return false, fmt.Errorf("'in' list holds a non-string")
				}
				if eqPtr(normFont(as), normFont(es)) {
					return true, nil
				}
			}
			return false, nil
		}
		for _, e := range list {
			if pyEqual(actual, e) {
				return true, nil
			}
		}
		return false, nil
	case "range":
		if actual == nil {
			return false, nil
		}
		list, ok := expected.([]any)
		if !ok || len(list) != 2 {
			return false, fmt.Errorf("'range' needs [min, max]")
		}
		lo, ok1 := asNumber(list[0])
		hi, ok2 := asNumber(list[1])
		a, ok3 := asNumber(actual)
		if !ok1 || !ok2 || !ok3 {
			return false, fmt.Errorf("'range' compares numbers")
		}
		return lo <= a && a <= hi, nil
	case "ge", "le":
		if actual == nil {
			return false, nil
		}
		a, ok1 := asNumber(actual)
		e, ok2 := asNumber(expected)
		if !ok1 || !ok2 {
			return false, fmt.Errorf("%q compares numbers", op)
		}
		if op == "ge" {
			return a >= e, nil
		}
		return a <= e, nil
	case "regex":
		if actual == nil {
			return false, nil
		}
		pat, ok := expected.(string)
		if !ok {
			return false, fmt.Errorf("'regex' needs a pattern")
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return false, err
		}
		return re.MatchString(pyStr(actual)), nil
	}
	return false, fmt.Errorf("unknown op %q", op)
}

func optStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func optNum(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func optBool(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}

// paraAttr is the paragraph property a prop check reads.
func paraAttr(p *Para, prop string) any {
	switch prop {
	case "text":
		return p.Text
	case "zone":
		return p.Zone
	case "alignment":
		return p.Alignment
	case "font_name":
		return optStr(p.FontName)
	case "size_pt":
		return optNum(p.SizePt)
	case "bold":
		return optBool(p.Bold)
	case "italic":
		return optBool(p.Italic)
	case "underline":
		return optBool(p.Underline)
	case "caps":
		return optBool(p.Caps)
	case "text_is_upper", "text_upper":
		return p.TextIsUpper
	case "indent_left_mm":
		return optNum(p.IndentLeftMM)
	case "indent_right_mm":
		return optNum(p.IndentRightMM)
	case "indent_first_line_mm":
		return optNum(p.IndentFirstLineMM)
	case "indent_hanging_mm":
		return optNum(p.IndentHangingMM)
	case "space_before_pt":
		return optNum(p.SpaceBeforePt)
	case "space_after_pt":
		return optNum(p.SpaceAfterPt)
	case "line_spacing_pt":
		return optNum(p.LineSpacingPt)
	case "line_spacing_multiple":
		return optNum(p.LineSpacingMult)
	case "page_break_before":
		return p.PageBreakBefore
	case "in_table":
		return p.InTable
	case "source":
		return p.Source
	}
	return nil
}

func sideAttr(s SideFormat, prop string) (any, bool) {
	if !s.Present {
		return nil, false
	}
	switch prop {
	case "font_name":
		return optStr(s.FontName), true
	case "size_pt":
		return optNum(s.SizePt), true
	case "bold":
		return optBool(s.Bold), true
	case "italic":
		return optBool(s.Italic), true
	case "underline":
		return optBool(s.Underline), true
	case "caps":
		return optBool(s.Caps), true
	}
	return nil, false
}

func sectionAttr(s *Section, prop string) any {
	switch prop {
	case "page_width_mm":
		return optNum(s.PageWidthMM)
	case "page_height_mm":
		return optNum(s.PageHeightMM)
	case "orientation":
		return optStr(s.Orientation)
	case "margin_top_mm":
		return optNum(s.MarginTopMM)
	case "margin_right_mm":
		return optNum(s.MarginRightMM)
	case "margin_bottom_mm":
		return optNum(s.MarginBottomMM)
	case "margin_left_mm":
		return optNum(s.MarginLeftMM)
	case "header_mm":
		return optNum(s.HeaderMM)
	case "footer_mm":
		return optNum(s.FooterMM)
	case "gutter_mm":
		return optNum(s.GutterMM)
	}
	return nil
}

type evalOut struct {
	status   string
	actual   any
	evidence []map[string]any
	note     string
}

func evalProp(l *Layout, seg *Segmentation, c RuleCheck) (evalOut, error) {
	comp := seg.Comp(c.Component)
	var paras []*Para
	for _, idx := range comp.Paras {
		if idx >= 0 && idx < len(l.Paragraphs) {
			paras = append(paras, l.Paragraphs[idx])
		}
	}
	if len(paras) == 0 {
		return evalOut{status: StatusSkip, note: fmt.Sprintf("component '%s' not found", c.Component)}, nil
	}
	actualOf := func(i int, p *Para) any {
		z := p.Zone
		if i < len(comp.Zones) {
			z = comp.Zones[i]
		}
		if c.Prop == "zone" {
			return z
		}
		if c.Prop == "text_upper" {
			return p.TextIsUpper
		}
		if p.Zone == ZoneSplit && (z == ZoneLeft || z == ZoneRight) {
			side := p.LeftProps
			if z == ZoneRight {
				side = p.RightProps
			}
			if v, ok := sideAttr(side, c.Prop); ok {
				return v
			}
			if c.Prop == "text" {
				if z == ZoneLeft {
					return p.LeftText
				}
				return p.RightText
			}
		}
		return paraAttr(p, c.Prop)
	}
	sat := func(i int, p *Para) (bool, error) {
		actual := actualOf(i, p)
		if c.Prop == "font_name" {
			if actual == nil {
				return false, nil
			}
		}
		return compare(c.Op, actual, c.Value)
	}
	var textParas []*Para
	for _, p := range paras {
		if strings.TrimSpace(p.Text) != "" {
			textParas = append(textParas, p)
		}
	}
	if len(textParas) == 0 {
		return evalOut{status: StatusSkip, note: fmt.Sprintf("component '%s' has no text paragraphs", c.Component)}, nil
	}
	nOK := 0
	evidence := []map[string]any{}
	for i, p := range textParas {
		ok, err := sat(i, p)
		if err != nil {
			return evalOut{}, err
		}
		if ok {
			nOK++
		} else if len(evidence) < 5 {
			evidence = append(evidence, map[string]any{
				"para": p.Index, "text": truncateRunes(strings.TrimSpace(p.Text), 80),
				"actual": actualOf(i, p),
			})
		}
	}
	var passed bool
	switch c.Mode {
	case "any":
		passed = nOK >= 1
	case "majority":
		passed = float64(nOK)/float64(len(textParas)) >= 0.7
	default:
		passed = nOK == len(textParas)
	}
	status := StatusPass
	if !passed {
		status = failStatus(c.Severity)
	}
	return evalOut{status: status, actual: map[string]any{"satisfied": nOK, "of": len(textParas)}, evidence: evidence}, nil
}

func evalPageSetup(l *Layout, c RuleCheck) (evalOut, error) {
	if len(l.Sections) == 0 {
		return evalOut{status: StatusSkip, note: "no section info"}, nil
	}
	actuals := make([]any, 0, len(l.Sections))
	ok := true
	for _, s := range l.Sections {
		a := sectionAttr(s, c.Prop)
		actuals = append(actuals, a)
		if a == nil {
			ok = false
			continue
		}
		good, err := compare(c.Op, a, c.Value)
		if err != nil {
			return evalOut{}, err
		}
		ok = ok && good
	}
	status := StatusPass
	if !ok {
		status = failStatus(c.Severity)
	}
	return evalOut{status: status, actual: actuals}, nil
}

func evalPresent(seg *Segmentation, component, op, severity string) evalOut {
	found := seg.Comp(component).Found
	status := StatusPass
	if found != (op == "present") {
		status = failStatus(severity)
	}
	actual := "missing"
	if found {
		actual = "found"
	}
	return evalOut{status: status, actual: actual}
}

func compZones(c *Component) map[string]bool {
	out := map[string]bool{}
	if len(c.Zones) > 0 {
		for _, z := range c.Zones {
			out[z] = true
		}
	} else if c.Zone != "" {
		out[c.Zone] = true
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// crossColumnPair: the two components sit in different zones of the SAME
// table — XML emits a table's left column before its right one, so element
// positions cannot order them.
func crossColumnPair(a, b *Component, seg *Segmentation) bool {
	if sameSet(compZones(a), compZones(b)) {
		return false
	}
	tables := func(c *Component) map[int]bool {
		out := map[int]bool{}
		for _, p := range c.Paras {
			if t := seg.ParaTable[p]; t != nil {
				out[*t] = true
			}
		}
		return out
	}
	ta, tb := tables(a), tables(b)
	for t := range ta {
		if tb[t] {
			return true
		}
	}
	return false
}

func evalOrder(seg *Segmentation, order []string) evalOut {
	type placed struct {
		name string
		pos  int
		comp *Component
	}
	var ps []placed
	for _, name := range order {
		c := seg.Comp(name)
		if c.Found && c.Pos != nil {
			ps = append(ps, placed{name, *c.Pos, c})
		}
	}
	violations := []map[string]any{}
	for i := 1; i < len(ps); i++ {
		prev, cur := ps[i-1], ps[i]
		if cur.pos < prev.pos && !crossColumnPair(cur.comp, prev.comp, seg) {
			violations = append(violations, map[string]any{
				"component": cur.name, "pos": cur.pos,
				"should_follow": prev.name, "should_follow_pos": prev.pos,
			})
		}
	}
	actual := make([]any, 0, len(ps))
	for _, p := range ps {
		actual = append(actual, []any{p.name, p.pos})
	}
	status := StatusPass
	if len(violations) > 0 {
		status = StatusFail
	}
	return evalOut{status: status, actual: actual, evidence: violations}
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Evaluate runs a rule set over a segmented document.
func Evaluate(l *Layout, seg *Segmentation, rs *RuleSet) []CheckResult {
	var out []CheckResult
	add := func(base CheckResult, r evalOut) {
		base.Status, base.Actual, base.Note = r.status, r.actual, r.note
		base.Evidence = r.evidence
		if base.Evidence == nil {
			base.Evidence = []map[string]any{}
		}
		out = append(out, base)
	}
	presence := func(comps []string, sev string) {
		for _, comp := range comps {
			r := evalPresent(seg, comp, "present", sev)
			// a missing optional component is not a finding: it only needs
			// to be right when present, which the prop checks cover
			if sev == "warn" && r.status == StatusWarn {
				r = evalOut{status: StatusSkip, actual: "missing",
					note: "thành phần không bắt buộc, không có trong văn bản"}
			}
			desc := fmt.Sprintf("Văn bản phải có thành phần '%s'", comp)
			if sev != "error" {
				desc = fmt.Sprintf("Văn bản có thành phần '%s' (không bắt buộc)", comp)
			}
			add(CheckResult{ID: "component." + comp + ".present", Desc: desc,
				Severity: sev, Component: ptr(comp)}, r)
		}
	}
	presence(rs.RequiredComponents, "error")
	presence(rs.OptionalComponents, "warn")

	for _, c := range rs.Checks {
		if c.Mode == "" {
			c.Mode = "all"
		}
		sev := c.Severity
		if sev == "" {
			sev = "error"
		}
		c.Severity = sev
		id := c.ID
		if id == "" {
			id = "?"
		}
		base := CheckResult{ID: id, Desc: c.Desc, Severity: sev, Component: strPtrOrNil(c.Component)}
		var r evalOut
		var err error
		switch kind := c.Kind; kind {
		case "present", "absent":
			r = evalPresent(seg, c.Component, c.Op, sev)
		case "page_setup":
			r, err = evalPageSetup(l, c)
		case "prop", "":
			r, err = evalProp(l, seg, c)
		case "stray_chars":
			r = evalStrayChars(l, c)
		default:
			r = evalOut{status: StatusSkip, note: fmt.Sprintf("unknown check kind '%s'", kind)}
		}
		if err != nil { // a malformed rule must not kill the report
			r = evalOut{status: StatusSkip, note: err.Error()}
		}
		add(base, r)
	}
	if len(rs.Order) > 0 {
		add(CheckResult{ID: "component.order", Desc: "Thứ tự các thành phần của văn bản",
			Severity: "error"}, evalOrder(seg, rs.Order))
	}
	return out
}

// strayCharRe: a symbol wedged between two letters ("Chính p`hủ") — a typo
// no reader would write.
var strayCharRe = regexp.MustCompile("\\p{L}[`~^\\\\|]\\p{L}")

func evalStrayChars(l *Layout, c RuleCheck) evalOut {
	evidence := []map[string]any{}
	n := 0
	for _, p := range l.Paragraphs {
		runes := []rune(p.Text)
		for _, loc := range strayCharRe.FindAllStringIndex(p.Text, -1) {
			n++
			if len(evidence) >= 5 {
				continue
			}
			start := utf8.RuneCountInString(p.Text[:loc[0]])
			end := start + utf8.RuneCountInString(p.Text[loc[0]:loc[1]])
			a, b := start-20, end+20
			if a < 0 {
				a = 0
			}
			if b > len(runes) {
				b = len(runes)
			}
			evidence = append(evidence, map[string]any{"para": p.Index,
				"text": strings.TrimSpace(string(runes[a:b])), "actual": p.Text[loc[0]:loc[1]]})
		}
	}
	status := StatusPass
	if n > 0 {
		status = failStatus(c.Severity)
	}
	return evalOut{status: status, actual: n, evidence: evidence}
}
