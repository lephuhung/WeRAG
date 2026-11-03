package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/mailer"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Frontend routes the mails link to (frontend-next): the SuperAdmin billing
// console and the "Mua hoặc gia hạn gói" page of the settings.
const (
	emailAdminBillingPath = "/platform/admin?section=billing"
	emailBuyPlanPath      = "/platform/system/workspace/billing"
)

// emailReceiptPath is the printable receipt page of a paid order
// (frontend-next app/billing/receipt/[code]).
func emailReceiptPath(code string) string {
	return "/billing/receipt/" + url.PathEscape(code)
}

const (
	// emailEventTimeout bounds one background notification (all its mails);
	// emailMailTimeout bounds each mail, so a slow one cannot starve the rest.
	emailEventTimeout = 10 * time.Minute
	emailMailTimeout  = 30 * time.Second
	// emailTestTimeout bounds the "Gửi thử" request.
	emailTestTimeout = 30 * time.Second
	// expiryReminderLookahead covers the largest reminder threshold plus
	// the day boundary.
	expiryReminderLookahead = 16 * 24 * time.Hour
	// Reminders go out in office hours, Vietnam time.
	expiryReminderFromHour = 8
	expiryReminderToHour   = 20
)

// EmailNotifier mails billing notifications through the SMTP account of the
// email.* system settings. Nothing is sent while SMTP is off or incomplete.
type EmailNotifier struct {
	settings interfaces.SystemSettingService
	users    interfaces.UserRepository
	members  interfaces.TenantMemberRepository
	tenants  interfaces.TenantRepository
	plans    interfaces.UsagePlanRepository
	store    interfaces.EmailNotificationRepository
	sender   mailer.Sender
	baseURL  func() string
	now      func() time.Time
	// async runs a background send; tests replace it to run inline.
	async func(fn func())
	wg    sync.WaitGroup
}

// NewEmailNotifier creates the notifier with the net/smtp sender.
func NewEmailNotifier(
	cfg *config.Config,
	settings interfaces.SystemSettingService,
	users interfaces.UserRepository,
	members interfaces.TenantMemberRepository,
	tenants interfaces.TenantRepository,
	plans interfaces.UsagePlanRepository,
	store interfaces.EmailNotificationRepository,
) *EmailNotifier {
	n := &EmailNotifier{
		settings: settings, users: users, members: members, tenants: tenants, plans: plans, store: store,
		sender: mailer.NewSMTPSender(),
		now:    time.Now,
		baseURL: func() string {
			// same lookup as the invite links: config, then the env at call time
			base := ""
			if cfg != nil {
				base = strings.TrimSpace(cfg.FrontendBaseURL)
			}
			if base == "" {
				base = strings.TrimSpace(os.Getenv("FRONTEND_BASE_URL"))
			}
			return strings.TrimRight(base, "/")
		},
	}
	n.async = func(fn func()) {
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			fn()
		}()
	}
	return n
}

// SMTPConfig reads the SMTP account from the system settings.
func (n *EmailNotifier) SMTPConfig(ctx context.Context) mailer.Config {
	s := n.settings
	return mailer.Config{
		Enabled:     s.GetBool(ctx, types.SMTPEnabledSettingKey, "", false),
		Host:        strings.TrimSpace(s.GetString(ctx, types.SMTPHostSettingKey, "", "")),
		Port:        int(s.GetInt(ctx, types.SMTPPortSettingKey, "", types.DefaultSMTPPort)),
		Username:    strings.TrimSpace(s.GetString(ctx, types.SMTPUsernameSettingKey, "", "")),
		Password:    s.GetString(ctx, types.SMTPPasswordSettingKey, "", ""),
		FromAddress: strings.TrimSpace(s.GetString(ctx, types.SMTPFromAddressSettingKey, "", "")),
		FromName:    strings.TrimSpace(s.GetString(ctx, types.SMTPFromNameSettingKey, "", "")),
		Security:    s.GetString(ctx, types.SMTPSecuritySettingKey, "", types.SMTPSecuritySTARTTLS),
	}
}

func (n *EmailNotifier) link(path string) string {
	base := n.baseURL()
	if base == "" {
		return ""
	}
	return base + path
}

// background runs fn detached from the request (its cancellation does not
// stop the mail) with a timeout; a panic is logged, never propagated.
func (n *EmailNotifier) background(ctx context.Context, what string, fn func(ctx context.Context)) {
	bg := context.WithoutCancel(ctx)
	n.async(func() {
		ctx, cancel := context.WithTimeout(bg, emailEventTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf(ctx, "[Email] %s panicked: %v", what, r)
			}
		}()
		fn(ctx)
	})
}

// Wait blocks until the background sends started so far are done (tests
// and graceful shutdown).
func (n *EmailNotifier) Wait() { n.wg.Wait() }

// sendEach mails c to every address separately (recipients do not see
// each other); returns how many went out and the last error.
func (n *EmailNotifier) sendEach(ctx context.Context, cfg mailer.Config, c emailContent, to []string) (int, error) {
	sent := 0
	var lastErr error
	for _, addr := range to {
		msg, err := c.render([]string{addr})
		if err != nil {
			return sent, err
		}
		mctx, cancel := context.WithTimeout(ctx, emailMailTimeout)
		err = n.sender.Send(mctx, cfg, msg)
		cancel()
		if err != nil {
			lastErr = err
			logger.Warnf(ctx, "[Email] %q to %s failed: %v", c.Subject, addr, err)
			continue
		}
		sent++
	}
	return sent, lastErr
}

// validEmail keeps addresses an SMTP server accepts.
func validEmail(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.ContainsAny(addr, "\r\n") {
		return false
	}
	a, err := mail.ParseAddress(addr)
	return err == nil && a.Address == addr
}

func addEmail(out []string, seen map[string]bool, u *types.User) []string {
	if u == nil || !u.IsActive || !validEmail(u.Email) {
		return out
	}
	key := strings.ToLower(strings.TrimSpace(u.Email))
	if seen[key] {
		return out
	}
	seen[key] = true
	return append(out, strings.TrimSpace(u.Email))
}

// superAdminEmails lists the active SuperAdmins with a usable email.
func (n *EmailNotifier) superAdminEmails(ctx context.Context) ([]string, error) {
	admins, _, err := n.users.ListSystemAdmins(ctx, 0, 500)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, u := range admins {
		out = addEmail(out, seen, u)
	}
	return out, nil
}

// scopeEmails lists who is told about a scope's package: the Tenant Admins
// of a tenant, the person of a personal package.
func (n *EmailNotifier) scopeEmails(ctx context.Context, scopeType, scopeID string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	switch scopeType {
	case types.UsageScopeUser:
		u, err := n.users.GetUserByID(ctx, scopeID)
		if err != nil {
			return nil, err
		}
		return addEmail(out, seen, u), nil
	case types.UsageScopeTenant:
		var tenantID uint64
		if _, err := fmt.Sscanf(scopeID, "%d", &tenantID); err != nil || tenantID == 0 {
			return nil, fmt.Errorf("bad tenant scope %q", scopeID)
		}
		ms, err := n.members.ListByTenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		var ids []string
		for _, m := range ms {
			if m.Status == types.TenantMemberStatusActive && m.Role.IsTenantAdmin() {
				ids = append(ids, m.UserID)
			}
		}
		if len(ids) == 0 {
			return nil, nil
		}
		users, err := n.users.GetUsersByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			out = addEmail(out, seen, users[id])
		}
		return out, nil
	}
	return nil, nil
}

func (n *EmailNotifier) scopeLabel(ctx context.Context, scopeType, scopeID string) string {
	switch scopeType {
	case types.UsageScopeTenant:
		var id uint64
		if _, err := fmt.Sscanf(scopeID, "%d", &id); err == nil && n.tenants != nil {
			if t, err := n.tenants.GetTenantByID(ctx, id); err == nil && t != nil && t.Name != "" {
				return "Tổ chức " + t.Name
			}
		}
		return "Tổ chức " + scopeID
	default:
		if u, err := n.users.GetUserByID(ctx, scopeID); err == nil && u != nil {
			if u.Username != "" {
				return "tài khoản " + u.Username
			}
			return "tài khoản " + u.Email
		}
		return "tài khoản " + scopeID
	}
}

// OrderCreated implements interfaces.BillingNotifier.
func (n *EmailNotifier) OrderCreated(ctx context.Context, order *types.UsageOrderView) {
	if order == nil || order.UsageOrder == nil {
		return
	}
	o := *order
	n.background(ctx, "order created", func(ctx context.Context) {
		cfg := n.SMTPConfig(ctx)
		if !cfg.Ready() {
			return
		}
		to, err := n.superAdminEmails(ctx)
		if err != nil {
			logger.Warnf(ctx, "[Email] order %s: list SuperAdmins: %v", o.OrderCode, err)
			return
		}
		if len(to) == 0 {
			return
		}
		_, _ = n.sendEach(ctx, cfg, orderCreatedEmail(&o, n.link(emailAdminBillingPath)), to)
	})
}

// OrderPaid implements interfaces.BillingNotifier.
func (n *EmailNotifier) OrderPaid(ctx context.Context, order *types.UsageOrderView) {
	if order == nil || order.UsageOrder == nil || order.Status != types.UsageOrderPaid {
		return
	}
	o := *order
	n.background(ctx, "order paid", func(ctx context.Context) {
		cfg := n.SMTPConfig(ctx)
		if !cfg.Ready() {
			return
		}
		buyer := o.CreatedBy
		if buyer == "" && o.ScopeType == types.UsageScopeUser {
			buyer = o.ScopeID
		}
		to := n.buyerEmails(ctx, &o, buyer)
		if len(to) == 0 {
			return
		}
		c := orderPaidEmail(&o, n.paidUntil(ctx, &o), n.link(emailBuyPlanPath))
		c.withReceipt(n.link(emailReceiptPath(o.OrderCode)))
		_, _ = n.sendEach(ctx, cfg, c, to)
	})
}

// buyerEmails is who hears that an order was paid: the buyer; when the
// buyer cannot be mailed (inactive, deleted, no email), the Tenant Admins
// of a tenant order instead.
func (n *EmailNotifier) buyerEmails(ctx context.Context, o *types.UsageOrderView, buyer string) []string {
	if buyer != "" {
		u, err := n.users.GetUserByID(ctx, buyer)
		if err != nil {
			logger.Warnf(ctx, "[Email] order %s: buyer %s: %v", o.OrderCode, buyer, err)
		}
		if to := addEmail(nil, map[string]bool{}, u); len(to) > 0 {
			return to
		}
	}
	if o.ScopeType != types.UsageScopeTenant {
		return nil
	}
	to, err := n.scopeEmails(ctx, o.ScopeType, o.ScopeID)
	if err != nil {
		logger.Warnf(ctx, "[Email] order %s: organization admins: %v", o.OrderCode, err)
	}
	return to
}

// paidUntil is the end of what a paid order bought: the subscription end,
// or for an add-on order the latest end of its add-ons.
func (n *EmailNotifier) paidUntil(ctx context.Context, o *types.UsageOrderView) *time.Time {
	if o.SubscriptionID == nil || n.plans == nil {
		return nil
	}
	sub, err := n.plans.GetSubscription(ctx, *o.SubscriptionID)
	if err != nil || sub == nil {
		return nil
	}
	if o.Kind != types.UsageOrderKindAddon || len(o.Addons) == 0 {
		return sub.EndsAt
	}
	held, err := n.plans.ListSubscriptionAddons(ctx, sub.ID)
	if err != nil {
		return sub.EndsAt
	}
	bought := map[string]bool{}
	for _, a := range o.Addons {
		bought[a.AgentID] = true
	}
	var end *time.Time
	for _, h := range held {
		if !bought[h.AgentID] {
			continue
		}
		e := h.EndsAt
		if e == nil {
			e = sub.EndsAt
		}
		if e != nil && (end == nil || e.After(*end)) {
			end = e
		}
	}
	if end == nil {
		return sub.EndsAt
	}
	return end
}

// SendTest mails the test message to addr synchronously and returns the
// SMTP error, for the "Gửi thử" button.
func (n *EmailNotifier) SendTest(ctx context.Context, addr string) error {
	cfg := n.SMTPConfig(ctx)
	if !cfg.Enabled {
		return errors.New("gửi email đang tắt")
	}
	if !cfg.Ready() {
		return errors.New("chưa nhập máy chủ SMTP hoặc địa chỉ người gửi")
	}
	if !validEmail(addr) {
		return errors.New("tài khoản của bạn chưa có địa chỉ email hợp lệ")
	}
	ctx, cancel := context.WithTimeout(ctx, emailTestTimeout)
	defer cancel()
	msg, err := testEmail(n.now()).render([]string{addr})
	if err != nil {
		return err
	}
	return n.sender.Send(ctx, cfg, msg)
}

// expiryReminderKey is unique per subscription, period end and threshold:
// a renewal moves the end, so the next period is reminded again.
func expiryReminderKey(subID string, endsAt time.Time, days int) string {
	return fmt.Sprintf("%s:%s:%d:%d", types.EmailNotificationSubscriptionExpiry, subID, endsAt.Unix(), days)
}

// skipExpiryReminder: the SuperAdmins' own package and the portal guests'
// package are not sold, so nobody is asked to renew them.
func skipExpiryReminder(sub *types.UsageSubscription) bool {
	if sub.PlanID == types.UsageGuestPlanID {
		return true
	}
	if sub.Plan != nil && (sub.Plan.Code == types.UsageSystemAdminPlanCode || sub.Plan.Code == types.UsageGuestPlanCode) {
		return true
	}
	return false
}

// RunExpiryReminders mails the reminders due now (14, 7 and 1 day(s)
// before a package's last usable day) and returns how many subscriptions
// were reminded. Each (subscription, period end, threshold) is claimed in
// email_notifications first, so it is mailed at most once across restarts
// and backends; a claim whose mails all failed is released for the next run.
func (n *EmailNotifier) RunExpiryReminders(ctx context.Context) (int, error) {
	cfg := n.SMTPConfig(ctx)
	if !cfg.Ready() {
		return 0, nil
	}
	now := n.now()
	// claims a crashed backend left mid-send are retried
	if released, err := n.store.ReleaseStale(ctx, types.EmailNotificationSubscriptionExpiry,
		now.Add(-types.EmailClaimStaleAfter)); err != nil {
		return 0, err
	} else if released > 0 {
		logger.Infof(ctx, "[Email] released %d stale expiry reminder claim(s)", released)
	}
	subs, err := n.store.ListSubscriptionsEndingBetween(ctx, now, now.Add(expiryReminderLookahead))
	if err != nil {
		return 0, err
	}
	reminded := 0
	for _, sub := range subs {
		if sub.EndsAt == nil || skipExpiryReminder(sub) {
			continue
		}
		daysLeft := types.DaysUntilLastUsableDayVN(now, *sub.EndsAt)
		due := types.DueExpiryReminder(daysLeft)
		if due == 0 {
			continue
		}
		key := expiryReminderKey(sub.ID, *sub.EndsAt, due)
		claimed, err := n.store.Claim(ctx, &types.EmailNotification{
			Kind: types.EmailNotificationSubscriptionExpiry, DedupeKey: key, RefID: sub.ID, CreatedAt: now,
		})
		if err != nil {
			return reminded, err
		}
		if !claimed {
			continue
		}
		to, err := n.scopeEmails(ctx, sub.ScopeType, sub.ScopeID)
		if err != nil {
			logger.Warnf(ctx, "[Email] expiry reminder %s: recipients: %v", sub.ID, err)
			_ = n.store.Release(ctx, key)
			continue
		}
		if len(to) == 0 {
			// nobody to tell; kept (not pending) so it is not retried
			_ = n.store.SetRecipients(ctx, key, types.EmailRecipientsNone)
			continue
		}
		planName := sub.PlanID
		if sub.Plan != nil && sub.Plan.Name != "" {
			planName = sub.Plan.Name
		}
		c := expiryReminderEmail(planName, n.scopeLabel(ctx, sub.ScopeType, sub.ScopeID), *sub.EndsAt, daysLeft,
			n.link(emailBuyPlanPath))
		sent, _ := n.sendEach(ctx, cfg, c, to)
		if sent == 0 {
			_ = n.store.Release(ctx, key)
			continue
		}
		_ = n.store.SetRecipients(ctx, key, sent)
		reminded++
	}
	return reminded, nil
}

// inReminderHours: reminders go out between 08:00 and 20:00 Vietnam time.
func inReminderHours(t time.Time) bool {
	h := t.In(types.UsageLocation()).Hour()
	return h >= expiryReminderFromHour && h < expiryReminderToHour
}

// StartExpiryReminders runs RunExpiryReminders every interval (first run
// after a minute) in office hours; stop ends the loop.
func (n *EmailNotifier) StartExpiryReminders(interval time.Duration) (stop func()) {
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-done:
				return
			case <-timer.C:
			}
			if inReminderHours(n.now()) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				if count, err := n.RunExpiryReminders(ctx); err != nil {
					logger.Warnf(ctx, "[Email] expiry reminders: %v", err)
				} else if count > 0 {
					logger.Infof(ctx, "[Email] expiry reminders sent for %d package(s)", count)
				}
				cancel()
			}
			timer.Reset(interval)
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
