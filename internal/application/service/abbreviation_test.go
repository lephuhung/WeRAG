package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type fakeAbbreviationRepo struct {
	interfaces.AbbreviationRepository
	rows      []*types.Abbreviation
	createN   int
	listCalls int
	listErr   error
}

func (r *fakeAbbreviationRepo) Create(_ context.Context, abbr *types.Abbreviation) error {
	r.createN++
	abbr.ID = "id-1"
	r.rows = append(r.rows, abbr)
	return nil
}

func (r *fakeAbbreviationRepo) ListByShortForm(_ context.Context, short string) ([]*types.Abbreviation, error) {
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*types.Abbreviation
	for _, row := range r.rows {
		if strings.EqualFold(row.ShortForm, short) {
			out = append(out, row)
		}
	}
	return out, nil
}

func TestAbbreviationSuggestCreatesPendingWithAttribution(t *testing.T) {
	repo := &fakeAbbreviationRepo{}
	svc := NewAbbreviationService(repo)
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "user-1")

	row, err := svc.Suggest(ctx, &types.AbbreviationCreateRequest{
		ShortForm: " UBND ", FullForm: "Ủy ban nhân dân",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if row.IsActive {
		t.Fatal("suggestion must be pending")
	}
	if row.SuggestedBy != "user-1" {
		t.Fatalf("SuggestedBy=%q", row.SuggestedBy)
	}
	if row.ShortForm != "UBND" {
		t.Fatalf("ShortForm=%q", row.ShortForm)
	}
	if repo.createN != 1 || repo.listCalls != 1 {
		t.Fatalf("create=%d list=%d", repo.createN, repo.listCalls)
	}
}

func TestAbbreviationSuggestIdempotentForIdenticalMeaning(t *testing.T) {
	repo := &fakeAbbreviationRepo{rows: []*types.Abbreviation{
		{ID: "existing", ShortForm: "UBND", FullForm: "Ủy Ban Nhân Dân", IsActive: false},
	}}
	svc := NewAbbreviationService(repo)

	row, err := svc.Suggest(context.Background(), &types.AbbreviationCreateRequest{
		ShortForm: "ubnd", FullForm: " ủy ban nhân dân ",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if row.ID != "existing" {
		t.Fatalf("expected the existing row, got %+v", row)
	}
	if repo.createN != 0 {
		t.Fatalf("must not duplicate, create=%d", repo.createN)
	}
}

func TestAbbreviationSuggestAllowsDifferentMeaning(t *testing.T) {
	repo := &fakeAbbreviationRepo{rows: []*types.Abbreviation{
		{ID: "existing", ShortForm: "BCH", FullForm: "Ban chấp hành"},
	}}
	svc := NewAbbreviationService(repo)

	row, err := svc.Suggest(context.Background(), &types.AbbreviationCreateRequest{
		ShortForm: "BCH", FullForm: "Bệnh viện C Hòa",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if row.FullForm != "Bệnh viện C Hòa" || repo.createN != 1 {
		t.Fatalf("different meaning must be stored: %+v create=%d", row, repo.createN)
	}
}

func TestAbbreviationSuggestValidatesRequest(t *testing.T) {
	svc := NewAbbreviationService(&fakeAbbreviationRepo{})
	for _, req := range []*types.AbbreviationCreateRequest{
		nil,
		{ShortForm: "  ", FullForm: "x"},
		{ShortForm: strings.Repeat("a", 51), FullForm: "x"},
		{ShortForm: "ABC", FullForm: "  "},
		{ShortForm: "ABC", FullForm: strings.Repeat("a", 256)},
	} {
		_, err := svc.Suggest(context.Background(), req)
		if err == nil {
			t.Fatalf("request must be rejected: %+v", req)
		}
		var appErr *werrors.AppError
		if !errors.As(err, &appErr) || appErr.HTTPCode != http.StatusBadRequest {
			t.Fatalf("validation must map to HTTP 400, got %v", err)
		}
	}
}

func TestAbbreviationCreateValidatesRequest(t *testing.T) {
	repo := &fakeAbbreviationRepo{}
	svc := NewAbbreviationService(repo)
	if _, err := svc.Create(context.Background(), &types.AbbreviationCreateRequest{
		ShortForm: "ABC", FullForm: " ",
	}, true); err == nil {
		t.Fatal("whitespace full form must be rejected")
	}
	row, err := svc.Create(context.Background(), &types.AbbreviationCreateRequest{
		ShortForm: "ABC", FullForm: "Alpha Beta Charlie",
	}, true)
	if err != nil || !row.IsActive {
		t.Fatalf("row=%+v err=%v", row, err)
	}
}
