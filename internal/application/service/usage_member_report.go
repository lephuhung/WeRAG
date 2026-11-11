package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// TenantMemberUsage is the member usage report of one organization for one
// Vietnam-calendar month ("" = this month).
//
// Each person's figures come from their "member" counters (the person inside
// this organization, types.UsageScopeMember); the "user" counters are not
// used because they add up every organization the person works in. A month
// with no member counter at all predates member counting: its figures are
// summed from the organization's chat history instead (Source "messages":
// questions and chat tokens, no OCR, no cost).
//
// Everyone who holds an active membership is listed, with zero usage when
// they asked nothing; someone with usage who is no longer a member (or a
// SuperAdmin who worked in the organization) is listed with Member=false.
// Machine principals (IM, API keys) have no line: their turns stay in
// Unattributed. withCost fills cost_micro_vnd (SuperAdmin only).
func (s *usagePlanService) TenantMemberUsage(
	ctx context.Context, tenantID uint64, month string, withCost bool,
) (*types.UsageMemberReport, error) {
	current := types.UsagePeriodKeysAt(s.now()).Month
	if month == "" {
		month = current
	}
	start, end, err := types.ParseUsageMonth(month)
	if err != nil {
		return nil, apperrors.NewBadRequestError("month must be YYYY-MM")
	}
	if month > current {
		return nil, apperrors.NewBadRequestError("month is in the future")
	}
	reader, ok := s.counters.(interfaces.UsageMemberReportRepository)
	if !ok {
		return nil, fmt.Errorf("usage counters cannot report per member")
	}
	if s.tenants != nil {
		t, err := s.tenants.GetTenantByID(ctx, tenantID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if t == nil || t.IsHiddenTenant() {
			// the public portal and config profiles have no members to report
			return nil, apperrors.NewNotFoundError("organization not found")
		}
	}

	out := &types.UsageMemberReport{
		TenantID: tenantID, Month: month, Timezone: types.UsageTimezone,
		Source: types.UsageMemberSourceCounters, CostVisible: withCost,
		Members:      []*types.UsageMemberUsage{},
		Total:        types.UsagePeriodTotals{PeriodKey: month},
		MembersTotal: types.UsagePeriodTotals{PeriodKey: month},
	}
	tenantRows, err := s.counters.Get(ctx, types.UsageScopeTenant, types.UsageTenantScopeID(tenantID), []string{month})
	if err != nil {
		return nil, err
	}
	for _, r := range tenantRows {
		out.Total.AddMetric(r.Metric, r.Value)
	}

	usage := map[string]*types.UsagePeriodTotals{}
	at := func(userID string) *types.UsagePeriodTotals {
		u := usage[userID]
		if u == nil {
			u = &types.UsagePeriodTotals{PeriodKey: month}
			usage[userID] = u
		}
		return u
	}
	memberRows, err := reader.ListMemberCounters(ctx, tenantID, month)
	if err != nil {
		return nil, err
	}
	for _, r := range memberRows {
		if uid := types.UsageMemberIDFromScope(tenantID, r.ScopeID); uid != "" {
			at(uid).AddMetric(r.Metric, r.Value)
		}
	}
	if len(memberRows) == 0 {
		sums, err := reader.SumMessageUsageByUser(ctx, tenantID, start, end)
		if err != nil {
			return nil, err
		}
		for uid, t := range sums {
			if types.IsSyntheticUserID(uid) {
				continue
			}
			out.Source = types.UsageMemberSourceMessages
			at(uid).Add(t)
		}
	}

	var members []*types.TenantMember
	if s.members != nil {
		if members, err = s.members.ListByTenant(ctx, tenantID); err != nil {
			return nil, err
		}
	}
	roles := make(map[string]types.TenantRole, len(members))
	ids := make([]string, 0, len(members)+len(usage))
	for _, m := range members {
		if m.Status != types.TenantMemberStatusActive || types.IsSyntheticUserID(m.UserID) {
			continue
		}
		if _, seen := roles[m.UserID]; !seen {
			ids = append(ids, m.UserID)
		}
		roles[m.UserID] = m.Role
	}
	for uid := range usage {
		if _, member := roles[uid]; !member {
			ids = append(ids, uid)
		}
	}

	users := map[string]*types.User{}
	if s.users != nil && len(ids) > 0 {
		if found, err := s.users.GetUsersByIDs(ctx, ids); err == nil {
			users = found
		} else {
			// names are a convenience: the figures still stand without them
			logger.Warnf(ctx, "member usage report: user lookup failed: tenant=%d err=%v", tenantID, err)
		}
	}

	var seats map[string]bool
	sub, err := s.repo.GetActiveSubscription(ctx, types.UsageScopeTenant, types.UsageTenantScopeID(tenantID))
	if err != nil {
		return nil, err
	}
	if sub != nil {
		v, err := s.view(ctx, sub)
		if err != nil {
			return nil, err
		}
		out.Subscription = v
		if sub.Mode == types.UsageModeSeat {
			list, err := s.repo.ListSeats(ctx, sub.ID)
			if err != nil {
				return nil, err
			}
			seats = make(map[string]bool, len(list))
			for _, seat := range list {
				seats[seat.UserID] = true
			}
		}
	}

	for _, uid := range ids {
		role, member := roles[uid]
		row := &types.UsageMemberUsage{UserID: uid, Role: role, Member: member}
		if u := users[uid]; u != nil {
			row.Username, row.Email = u.Username, u.Email
		}
		if seats != nil {
			has := seats[uid]
			row.HasSeat = &has
		}
		if member {
			own, err := s.repo.GetActiveSubscription(ctx, types.UsageScopeUser, uid)
			if err != nil {
				return nil, err
			}
			if own != nil && own.Plan != nil {
				row.PersonalPlan = own.Plan.Name
			}
		}
		if u := usage[uid]; u != nil {
			row.Usage = *u
		} else {
			row.Usage = types.UsagePeriodTotals{PeriodKey: month}
		}
		if !withCost {
			row.Usage.CostMicroVND = 0
		}
		out.MembersTotal.Add(row.Usage)
		out.Members = append(out.Members, row)
	}
	if !withCost {
		out.Total.CostMicroVND = 0
	}
	out.Unattributed = out.Total.MinusFloor(out.MembersTotal)

	sort.SliceStable(out.Members, func(i, j int) bool {
		a, b := out.Members[i], out.Members[j]
		if a.Usage.Tokens != b.Usage.Tokens {
			return a.Usage.Tokens > b.Usage.Tokens
		}
		if a.Usage.Questions != b.Usage.Questions {
			return a.Usage.Questions > b.Usage.Questions
		}
		if a.Member != b.Member {
			return a.Member
		}
		return memberLabel(a) < memberLabel(b)
	})
	return out, nil
}

// memberLabel is the name a report line is shown and ordered by.
func memberLabel(m *types.UsageMemberUsage) string {
	switch {
	case m.Username != "":
		return m.Username
	case m.Email != "":
		return m.Email
	}
	return m.UserID
}
