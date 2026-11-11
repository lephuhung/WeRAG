package service

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/Tencent/WeKnora/internal/application/access"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// answerQualityService serves the admins' answer-quality inbox: disliked
// answers and ungrounded questions. Privacy (product owner decision): a
// Tenant Admin reads the content of their own organization, a SuperAdmin
// that of every organization; Members, guests and API keys never. The
// scope is decided here from the caller, whatever the request asks.
type answerQualityService struct {
	repo interfaces.AnswerQualityRepository
	now  func() time.Time
}

// NewAnswerQualityService creates the service.
func NewAnswerQualityService(repo interfaces.AnswerQualityRepository) interfaces.AnswerQualityService {
	return &answerQualityService{repo: repo, now: time.Now}
}

// answerQualityCaller is who asks and the organization they may read
// (allTenants for the SuperAdmin console).
type answerQualityCaller struct {
	userID     string
	tenantID   uint64
	allTenants bool
}

func (s *answerQualityService) caller(ctx context.Context, platform bool) (answerQualityCaller, error) {
	if _, isKey := types.TenantAPIKeyScopeFromContext(ctx); isKey {
		return answerQualityCaller{}, apperrors.NewForbiddenError("API keys cannot read the answer-quality inbox")
	}
	caller := types.CallerFromContext(ctx)
	if platform {
		if !access.IsExplicitHumanSuperAdmin(ctx, caller) {
			return answerQualityCaller{}, apperrors.NewForbiddenError("SuperAdmin required")
		}
		return answerQualityCaller{userID: caller.UserID, allTenants: true}, nil
	}
	if !access.IsAuthenticatedHuman(ctx, caller) {
		return answerQualityCaller{}, apperrors.NewForbiddenError("organization admin required")
	}
	tenantID, _ := types.TenantIDFromContext(ctx)
	superAdmin := types.IsSystemAdminFromContext(ctx)
	if tenantID == 0 || (!superAdmin && (!caller.Role.IsTenantAdmin() || caller.TenantID != tenantID)) {
		return answerQualityCaller{}, apperrors.NewForbiddenError("organization admin required")
	}
	return answerQualityCaller{userID: caller.UserID, tenantID: tenantID}, nil
}

// scoped applies the caller's scope to f: the console keeps the requested
// organization (0 = all), an organization's admin always reads their own.
func (s *answerQualityService) scoped(ctx context.Context, platform bool, f types.AnswerQualityFilter) (types.AnswerQualityFilter, error) {
	c, err := s.caller(ctx, platform)
	if err != nil {
		return f, err
	}
	if c.allTenants {
		f.AllTenants = f.TenantID == 0
	} else {
		f.TenantID, f.AllTenants = c.tenantID, false
	}
	if f.Limit <= 0 || f.Limit > types.AnswerQualityListMax {
		f.Limit = 20
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return f, nil
}

func (s *answerQualityService) page(
	ctx context.Context, kind string, f types.AnswerQualityFilter,
	list func(context.Context, types.AnswerQualityFilter) ([]*types.AnswerQualityRow, int64, error),
) (*types.AnswerQualityPage, error) {
	rows, total, err := list(ctx, f)
	if err != nil {
		return nil, err
	}
	agents, err := s.repo.Agents(ctx, kind, f)
	if err != nil {
		return nil, err
	}
	return &types.AnswerQualityPage{Items: rows, Total: total, Agents: agents}, nil
}

func (s *answerQualityService) ListDisliked(
	ctx context.Context, platform bool, f types.AnswerQualityFilter,
) (*types.AnswerQualityPage, error) {
	f, err := s.scoped(ctx, platform, f)
	if err != nil {
		return nil, err
	}
	f.Reason = ""
	return s.page(ctx, types.AnswerQualityKindDisliked, f, s.repo.ListDisliked)
}

func (s *answerQualityService) ListUngrounded(
	ctx context.Context, platform bool, f types.AnswerQualityFilter,
) (*types.AnswerQualityPage, error) {
	f, err := s.scoped(ctx, platform, f)
	if err != nil {
		return nil, err
	}
	f.HasComment = false
	return s.page(ctx, types.AnswerQualityKindUngrounded, f, s.repo.ListUngrounded)
}

func (s *answerQualityService) GroupUngrounded(
	ctx context.Context, platform bool, f types.AnswerQualityFilter,
) (*types.UngroundedGroupPage, error) {
	f, err := s.scoped(ctx, platform, f)
	if err != nil {
		return nil, err
	}
	f.HasComment = false
	rows, err := s.repo.UngroundedMarkers(ctx, f, types.UngroundedGroupScanMax+1)
	if err != nil {
		return nil, err
	}
	out := &types.UngroundedGroupPage{}
	if len(rows) > types.UngroundedGroupScanMax {
		rows, out.Truncated = rows[:types.UngroundedGroupScanMax], true
	}
	out.Rows = len(rows)
	groups := GroupUngroundedQuestions(rows)
	out.Total = len(groups)
	start := min(f.Offset, len(groups))
	end := min(start+f.Limit, len(groups))
	out.Items = groups[start:end]
	return out, nil
}

// GroupUngroundedQuestions groups rows (newest first) by
// NormalizeQuestion, most frequent first, ties by the latest. Rows with no
// question are left out.
func GroupUngroundedQuestions(rows []*types.AnswerQualityRow) []*types.UngroundedQuestionGroup {
	byKey := map[string]*types.UngroundedQuestionGroup{}
	orgs := map[string]map[uint64]bool{}
	var groups []*types.UngroundedQuestionGroup
	for _, r := range rows {
		key := NormalizeQuestion(r.Question)
		if key == "" {
			continue
		}
		g := byKey[key]
		if g == nil {
			g = &types.UngroundedQuestionGroup{Key: key, Reasons: map[string]int{}}
			byKey[key] = g
			orgs[key] = map[uint64]bool{}
			groups = append(groups, g)
		}
		g.Count++
		if r.Reason != "" {
			g.Reasons[r.Reason]++
		}
		if !orgs[key][r.TenantID] {
			orgs[key][r.TenantID] = true
			g.Organizations++
		}
		if g.LastAt.IsZero() || r.CreatedAt.After(g.LastAt) {
			g.LastAt, g.Sample, g.MessageID = r.CreatedAt, strings.TrimSpace(r.Question), r.MessageID
		}
		if g.LegalRequest == "" && r.LegalRequest != "" {
			g.LegalRequest = r.LegalRequest
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].LastAt.After(groups[j].LastAt)
	})
	return groups
}

// normalizedQuestionMaxRunes caps a grouping key.
const normalizedQuestionMaxRunes = 300

// NormalizeQuestion is the grouping key of a question: NFC, lower case,
// punctuation dropped (but "/", "-" and "." inside a token, which số hiệu
// and dates use), spaces collapsed. Diacritics are kept: in Vietnamese they
// change the word.
func NormalizeQuestion(q string) string {
	q = strings.ToLower(norm.NFC.String(q))
	var b strings.Builder
	runes := []rune(q)
	for i, r := range runes {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			b.WriteRune(r)
		case (r == '/' || r == '-' || r == '.') && i > 0 && i+1 < len(runes) &&
			isWordRune(runes[i-1]) && isWordRune(runes[i+1]):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	key := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(key) > normalizedQuestionMaxRunes {
		key = string([]rune(key)[:normalizedQuestionMaxRunes])
	}
	return key
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func (s *answerQualityService) Review(
	ctx context.Context, platform bool, messageID string, in types.AnswerQualityReviewInput,
) (*types.AnswerQualityReview, error) {
	c, err := s.caller(ctx, platform)
	if err != nil {
		return nil, err
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || len(messageID) > 36 {
		return nil, apperrors.NewBadRequestError("invalid message id")
	}
	if !types.ValidAnswerQualityKind(in.Kind) {
		return nil, apperrors.NewBadRequestError("kind must be disliked or ungrounded")
	}
	if in.Cause != nil && !types.ValidAnswerQualityCause(in.Kind, strings.TrimSpace(*in.Cause)) {
		return nil, apperrors.NewBadRequestError("unknown cause for this list")
	}
	if in.Note != nil && utf8.RuneCountInString(*in.Note) > types.AnswerQualityNoteMaxRunes {
		return nil, apperrors.NewBadRequestError("note is too long")
	}
	target, err := s.repo.Target(ctx, messageID)
	if err != nil {
		return nil, err
	}
	// another organization's message is "not found", not "forbidden": its
	// existence is not told
	if target == nil || (!c.allTenants && target.TenantID != c.tenantID) {
		return nil, apperrors.NewNotFoundError("message not found")
	}
	inList := target.Role == "assistant" && ((in.Kind == types.AnswerQualityKindDisliked && target.Feedback == types.MessageFeedbackDown) ||
		(in.Kind == types.AnswerQualityKindUngrounded && target.Ungrounded))
	if !inList {
		return nil, apperrors.NewBadRequestError("the message is not in this list")
	}
	rv, err := s.repo.GetReview(ctx, messageID, in.Kind)
	if err != nil {
		return nil, err
	}
	if rv == nil {
		rv = &types.AnswerQualityReview{MessageID: messageID, Kind: in.Kind}
	}
	now := s.now()
	rv.TenantID = target.TenantID
	if in.Cause != nil {
		rv.Cause = strings.TrimSpace(*in.Cause)
	}
	if in.Note != nil {
		rv.Note = strings.TrimSpace(*in.Note)
	}
	if in.Reviewed != nil {
		switch {
		case *in.Reviewed && !rv.Reviewed:
			rv.Reviewed, rv.ReviewedBy, rv.ReviewedAt = true, c.userID, &now
		case !*in.Reviewed:
			rv.Reviewed, rv.ReviewedBy, rv.ReviewedAt = false, "", nil
		}
	}
	rv.UpdatedBy, rv.UpdatedAt = c.userID, now
	if err := s.repo.SaveReview(ctx, rv); err != nil {
		return nil, err
	}
	return rv, nil
}
