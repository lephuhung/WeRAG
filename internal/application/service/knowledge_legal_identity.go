package service

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal"
)

// legalIdentityHeaderChars bounds the header region scanned for the
// document's own số hiệu and tên loại.
const legalIdentityHeaderChars = 2500

// applyLegalIdentity fills the profile's DocumentNumber / DocType from the
// document header and reconciles them with the model's answer:
//
//   - the deterministic header parse wins when it finds something — it reads
//     the "Số:" line and the tên loại line literally;
//   - otherwise the model's number is kept only when it occurs in the text
//     the model was given (spaces ignored), so an invented number is dropped;
//   - the model's doc_type is kept as-is when the header yields no type
//     (Normalize canonicalises Vietnamese type names);
//   - a type ký hiệu that lost its Đ is restored from the resolved type
//     (RestoreDocumentNumberSymbol).
//
// docText is the reconstructed document, sourceText what the model saw
// (document plus user metadata). Returns the (possibly new) profile, or nil
// when nothing usable remains.
func applyLegalIdentity(profile *types.KnowledgeProfile, docText, sourceText string) *types.KnowledgeProfile {
	header := docText
	if len(header) > legalIdentityHeaderChars {
		header = header[:legalIdentityHeaderChars]
	}
	detNumber := vietnamese_legal.NormalizeOwnDocumentNumber(vietnamese_legal.RecoverDocumentNumber(header))
	detType := vietnamese_legal.DetectDocType(header)

	if profile == nil {
		if detNumber == "" && detType.Slug == "" {
			return nil
		}
		profile = &types.KnowledgeProfile{}
	}

	switch {
	case detNumber != "":
		profile.DocumentNumber = detNumber
	default:
		n := vietnamese_legal.NormalizeOwnDocumentNumber(profile.DocumentNumber)
		if n == "" || !containsIgnoringSpace(sourceText, n) {
			n = ""
		}
		profile.DocumentNumber = n
	}
	if d := vietnamese_legal.DocTypeBySlug(detType.Slug); d != nil {
		profile.DocType = d.Name
	}
	profile = profile.Normalize()
	if profile != nil && profile.DocumentNumber != "" {
		// Normalize resolved DocTypeCode from the final doc_type; use it to
		// repair an OCR-damaged ký hiệu ("361/2025/ND-CP" → "NĐ-CP").
		profile.DocumentNumber = vietnamese_legal.RestoreDocumentNumberSymbol(
			profile.DocumentNumber, profile.DocTypeCode)
	}
	return profile
}

// containsIgnoringSpace reports whether needle occurs in haystack once all
// whitespace is removed from both, case-insensitively ("45 / KH-UBND" in the
// text matches "45/KH-UBND").
func containsIgnoringSpace(haystack, needle string) bool {
	squash := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }
	return needle != "" && strings.Contains(squash(haystack), squash(needle))
}
