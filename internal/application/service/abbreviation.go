package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/vietnamese_legal/abbreviation"
)

// abbreviationDetectTimeout bounds one decision-model call during
// detection; on timeout the heuristic takes over.
const abbreviationDetectTimeout = 5 * time.Second

type abbreviationService struct {
	repo   interfaces.AbbreviationRepository
	models interfaces.ModelService
}

// NewAbbreviationService creates the abbreviation service. models may be
// nil; candidate detection then always uses the heuristic.
func NewAbbreviationService(repo interfaces.AbbreviationRepository, models interfaces.ModelService) interfaces.AbbreviationService {
	return &abbreviationService{repo: repo, models: models}
}

// DetectCandidates asks the tenant's Decision model (Jev / Clef) which
// tokens are abbreviations. With no Decision model configured, or on any
// model error, it returns the heuristic detection so chat never blocks.
func (s *abbreviationService) DetectCandidates(ctx context.Context, text string) types.AbbreviationDetection {
	if s.models == nil {
		return types.AbbreviationDetection{}
	}
	decider, err := s.models.GetActiveDecisionModel(ctx)
	if err != nil {
		logger.Warnf(ctx, "abbreviation detection: decision model unavailable, using heuristic: %v", err)
		return types.AbbreviationDetection{}
	}
	if decider == nil {
		return types.AbbreviationDetection{}
	}
	detectCtx, cancel := context.WithTimeout(ctx, abbreviationDetectTimeout)
	defer cancel()
	det, err := abbreviation.Detect(detectCtx, abbreviation.NewDecisionDetector(decider), text)
	if err != nil {
		logger.Warnf(ctx, "abbreviation detection: %s failed, using heuristic: %v", decider.Provider(), err)
	}
	return det
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
	userID, _ := types.UserIDFromContext(ctx)
	return s.repo.SuggestUnique(ctx, &types.Abbreviation{
		ShortForm: short, FullForm: full, Description: req.Description, SuggestedBy: userID,
	})
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
