package service

import (
	"context"
	"strings"
	"unicode/utf8"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type abbreviationService struct {
	repo interfaces.AbbreviationRepository
}

// NewAbbreviationService creates the abbreviation service.
func NewAbbreviationService(repo interfaces.AbbreviationRepository) interfaces.AbbreviationService {
	return &abbreviationService{repo: repo}
}

// Suggest creates an inactive abbreviation suggestion attributed to the
// caller in ctx. Activation is an admin-only step done through Update.
func (s *abbreviationService) Suggest(
	ctx context.Context, req *types.AbbreviationCreateRequest,
) (*types.Abbreviation, error) {
	short, full, err := normalizeAbbreviationCreate(req)
	if err != nil {
		return nil, err
	}
	existing, err := s.repo.ListByShortForm(ctx, short)
	if err != nil {
		return nil, err
	}
	for _, row := range existing {
		if row != nil && strings.EqualFold(strings.TrimSpace(row.FullForm), full) {
			return row, nil
		}
	}
	return s.create(ctx, short, full, req.Description, false)
}

// Create inserts a row with an explicit active flag.
func (s *abbreviationService) Create(
	ctx context.Context, req *types.AbbreviationCreateRequest, active bool,
) (*types.Abbreviation, error) {
	short, full, err := normalizeAbbreviationCreate(req)
	if err != nil {
		return nil, err
	}
	return s.create(ctx, short, full, req.Description, active)
}

func normalizeAbbreviationCreate(req *types.AbbreviationCreateRequest) (string, string, error) {
	if req == nil {
		return "", "", werrors.NewBadRequestError("abbreviation request is required")
	}
	short := strings.TrimSpace(req.ShortForm)
	full := strings.TrimSpace(req.FullForm)
	if short == "" {
		return "", "", werrors.NewBadRequestError("short_form is required")
	}
	if utf8.RuneCountInString(short) > 50 {
		return "", "", werrors.NewBadRequestError("short_form must be at most 50 characters")
	}
	if full == "" {
		return "", "", werrors.NewBadRequestError("full_form is required")
	}
	if utf8.RuneCountInString(full) > 255 {
		return "", "", werrors.NewBadRequestError("full_form must be at most 255 characters")
	}
	return short, full, nil
}

func (s *abbreviationService) create(
	ctx context.Context, short, full, description string, active bool,
) (*types.Abbreviation, error) {
	userID, _ := types.UserIDFromContext(ctx)
	abbr := &types.Abbreviation{
		ShortForm:   short,
		FullForm:    full,
		Description: description,
		IsActive:    active,
		SuggestedBy: userID,
	}
	if err := s.repo.Create(ctx, abbr); err != nil {
		return nil, err
	}
	return abbr, nil
}

func (s *abbreviationService) Get(ctx context.Context, id string) (*types.Abbreviation, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *abbreviationService) List(
	ctx context.Context, search string, isActive *bool, page, perPage int,
) ([]*types.Abbreviation, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	return s.repo.List(ctx, search, isActive, (page-1)*perPage, perPage)
}

func (s *abbreviationService) Update(
	ctx context.Context, id string, req *types.AbbreviationUpdateRequest,
) (*types.Abbreviation, error) {
	abbr, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if abbr == nil {
		return nil, nil
	}
	if req.ShortForm != nil {
		abbr.ShortForm = strings.TrimSpace(*req.ShortForm)
	}
	if req.FullForm != nil {
		abbr.FullForm = strings.TrimSpace(*req.FullForm)
	}
	if req.Description != nil {
		abbr.Description = *req.Description
	}
	if req.IsActive != nil {
		abbr.IsActive = *req.IsActive
	}
	if err := s.repo.Update(ctx, abbr); err != nil {
		return nil, err
	}
	return abbr, nil
}

func (s *abbreviationService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *abbreviationService) ListActive(ctx context.Context) ([]*types.Abbreviation, error) {
	return s.repo.ListActive(ctx)
}

func (s *abbreviationService) ListByShortForm(
	ctx context.Context, shortForm string,
) ([]*types.Abbreviation, error) {
	return s.repo.ListByShortForm(ctx, shortForm)
}
