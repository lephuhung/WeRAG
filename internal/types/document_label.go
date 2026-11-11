package types

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Kinds of document assistant labels (DocumentLabel.Kind).
const (
	DocumentLabelSpelling = "spelling" // document_spelling_feedback
	DocumentLabelFormat   = "format"   // document_format_feedback
)

// DocumentLabelQuery filters the labels the users and the spelling verifier
// gave on the document assistant's spelling underlines and NĐ30 format
// check reports. TenantID 0 means every organization (SuperAdmin only; the
// handler enforces it). From is inclusive, To exclusive.
type DocumentLabelQuery struct {
	TenantID uint64
	Kind     string // "", DocumentLabelSpelling or DocumentLabelFormat
	Verdict  string
	Source   string // spelling: user | verifier; format rows are all "user"
	Model    string
	From     *time.Time
	To       *time.Time
	Offset   int
	Limit    int
}

// DocumentLabel is one label, the same shape for both kinds: the paragraph
// the finding was on, where in it the flagged text is, what the model
// suggested and what was said about it. It is also one line of the JSONL
// export, so it carries no user identity.
//
// SpanStart/SpanEnd are offsets in Unicode code points (runes) into
// Paragraph, end exclusive; nil when the flagged text cannot be found in
// the paragraph as stored (or there is no paragraph: a format verdict on
// the whole report, or a finding with no paragraph anchor).
type DocumentLabel struct {
	ID               string    `json:"id"`
	Kind             string    `json:"kind"`
	OrganizationID   uint64    `json:"organization_id"`
	OrganizationName string    `json:"organization_name,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	Paragraph        string    `json:"paragraph"`
	SpanStart        *int      `json:"span_start"`
	SpanEnd          *int      `json:"span_end"`
	FlaggedText      string    `json:"flagged_text"`
	Suggestion       string    `json:"suggestion"`
	// Current is what the format checker measured (format only).
	Current string `json:"current,omitempty"`
	Verdict string `json:"verdict"`
	// Source is who gave the verdict: "user", or "verifier" for a spelling
	// finding the verifier model rejected (a model label, not a human one).
	Source string `json:"source"`
	// CheckID is the NĐ30 check (format only; spelling has no rule ids).
	CheckID      string `json:"check_id"`
	FindingID    string `json:"finding_id,omitempty"`
	FindingTitle string `json:"finding_title,omitempty"`
	DocumentType string `json:"document_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Note         string `json:"note,omitempty"`
	Model        string `json:"model"`
}

// SpellingLabel converts a spelling feedback row. The span is the
// TextOccurrence-th instance of the flagged text in the paragraph (the
// spelling tool counts instances the same way), else its first instance.
func SpellingLabel(r *DocumentSpellingFeedback) DocumentLabel {
	l := DocumentLabel{
		ID: "spelling:" + strconv.FormatUint(r.ID, 10), Kind: DocumentLabelSpelling,
		OrganizationID: r.TenantID, CreatedAt: r.CreatedAt,
		Paragraph: r.Paragraph, FlaggedText: r.Wrong, Suggestion: r.Suggestion,
		Verdict: r.Verdict, Source: r.Source, Note: r.Note, Model: r.Model,
	}
	l.SpanStart, l.SpanEnd = LabelSpan(r.Paragraph, r.Wrong, r.TextOccurrence)
	return l
}

// FormatLabel converts a format feedback row. The paragraph, flagged text
// and expected value come from the finding in the kept report: the
// paragraph is the finding's anchor (its text as the checker normalised
// it), the flagged text the part of it the finding selects, else the whole
// paragraph. A verdict on the whole report has none of these.
func FormatLabel(r *DocumentFormatFeedback) DocumentLabel {
	l := DocumentLabel{
		ID: "format:" + strconv.FormatUint(r.ID, 10), Kind: DocumentLabelFormat,
		OrganizationID: r.TenantID, CreatedAt: r.CreatedAt,
		Verdict: r.Verdict, Source: SpellingFeedbackSourceUser,
		CheckID: r.CheckID, FindingID: r.FindingID, FindingTitle: r.FindingTitle,
		DocumentType: r.DocumentType, Scope: r.Scope, Note: r.Note, Model: r.Model,
	}
	if r.FindingID == "" || len(r.Report) == 0 {
		return l
	}
	var report struct {
		Findings []DocumentFormatFinding `json:"findings"`
	}
	if json.Unmarshal(r.Report, &report) != nil {
		return l
	}
	for _, f := range report.Findings {
		if f.ID != r.FindingID {
			continue
		}
		l.Suggestion, l.Current = f.Expected, f.Current
		if f.Anchor == nil || f.Anchor.Text == "" {
			break
		}
		l.Paragraph = f.Anchor.Text
		if f.Text != "" {
			l.FlaggedText = f.Text
			l.SpanStart, l.SpanEnd = LabelSpan(l.Paragraph, f.Text, f.TextOccurrence)
		} else {
			l.FlaggedText = l.Paragraph
			start, end := 0, utf8.RuneCountInString(l.Paragraph)
			l.SpanStart, l.SpanEnd = &start, &end
		}
		break
	}
	return l
}

// LabelSpan finds the occurrence-th (1-based, non-overlapping, like
// strings.Count) instance of text in paragraph, else its first one, and
// returns its rune offsets; nil when text is not in paragraph.
func LabelSpan(paragraph, text string, occurrence int) (*int, *int) {
	if text == "" || paragraph == "" {
		return nil, nil
	}
	at, first := -1, -1
	for from, n := 0, 0; ; {
		i := strings.Index(paragraph[from:], text)
		if i < 0 {
			break
		}
		n++
		if first < 0 {
			first = from + i
		}
		if n == max(occurrence, 1) {
			at = from + i
			break
		}
		from += i + len(text)
	}
	if at < 0 {
		at = first
	}
	if at < 0 {
		return nil, nil
	}
	start := utf8.RuneCountInString(paragraph[:at])
	end := start + utf8.RuneCountInString(text)
	return &start, &end
}
