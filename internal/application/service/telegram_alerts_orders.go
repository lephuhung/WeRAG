package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/alerting/telegram"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Order buttons on the Telegram alerts: a SuperAdmin who checked the bank
// account confirms a pending order as paid (or cancels it) from the alert.
//
// The press acts without a web session or 2FA, so it is guarded:
//   - the presser must have linked their own Telegram account with a
//     personal code (private chat, can_act) to an account that is still an
//     active SuperAdmin; anyone else (a group member, a colleague who only
//     receives alerts) is refused, also for "Thôi". The order records that
//     SuperAdmin as its confirmer, and an audit row (billing.order_*_telegram)
//     records the Telegram user and chat;
//   - every action is asked again: the question carries a one-time nonce
//     (10 minutes) bound to the presser, the chat and the question message,
//     so "Chắc chắn" cannot be sent without the question, by someone else,
//     or onto another message;
//   - the press must come from a chat that still gets alerts.
//
// Callback data: "oc:<code>" / "ox:<code>" ask to confirm / cancel;
// "ocy:<nonce>" / "oxy:<nonce>" do it, "no:<nonce>" dismisses the question.

const (
	tgActConfirmAsk = "oc"
	tgActConfirm    = "ocy"
	tgActCancelAsk  = "ox"
	tgActCancel     = "oxy"
	tgActDismiss    = "no"

	telegramAskTTL      = 10 * time.Minute
	telegramAskRedisKey = "telegram_alerts:ask:"
)

var (
	telegramOrderCodeRe = regexp.MustCompile(`^[A-Z0-9]{4,24}$`)
	telegramNonceRe     = regexp.MustCompile(`^[A-Z0-9]{10}$`)
)

// telegramAsk is a pending question: what it does, to which order, and the
// one press it accepts (Telegram user, chat, question message).
type telegramAsk struct {
	Act  string `json:"act"`
	Code string `json:"code"`
	User int64  `json:"user"`
	Chat int64  `json:"chat"`
	Msg  int64  `json:"msg"`
}

// SetOrderActions lets the bot act on orders (the order buttons); without
// it the alerts carry no buttons. Set at startup: the billing service is
// decorated with the bot itself, so it cannot be a constructor argument.
func (a *TelegramAlerts) SetOrderActions(orders interfaces.UsageBillingService, audit interfaces.AuditLogService) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orders, a.audit = orders, audit
}

func (a *TelegramAlerts) orderService() interfaces.UsageBillingService {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.orders
}

// orderButtons is the button row of a pending order, nil when the bot
// cannot act on orders.
func (a *TelegramAlerts) orderButtons(code string) [][]telegram.Button {
	if a == nil || code == "" || a.orderService() == nil {
		return nil
	}
	return [][]telegram.Button{telegramOrderRow(code)}
}

func telegramOrderRow(code string) []telegram.Button {
	return []telegram.Button{
		{Text: "✅ Đã nhận tiền " + code, Data: tgActConfirmAsk + ":" + code},
		{Text: "✖ Hủy đơn", Data: tgActCancelAsk + ":" + code},
	}
}

// digestButtons: the new-orders digest gets one row per order it names.
func (a *TelegramAlerts) digestButtons(n types.UserNotification) [][]telegram.Button {
	if n.Kind != types.UserNotificationPaymentOrderPending || a.orderService() == nil || len(n.Payload) == 0 {
		return nil
	}
	var p types.UserNotificationQueuePayload
	if json.Unmarshal(n.Payload, &p) != nil {
		return nil
	}
	var rows [][]telegram.Button
	for _, it := range p.Items {
		if it.Untitled || it.Ref == "" {
			continue
		}
		rows = append(rows, telegramOrderRow(it.Ref))
	}
	return rows
}

// telegramSuperAdmin is the SuperAdmin a Telegram user acts as: the
// account whose personal code linked that user's private chat with the bot
// (can_act), while it is an active SuperAdmin. nil when none.
func (a *TelegramAlerts) telegramSuperAdmin(ctx context.Context, telegramUserID int64) (*types.User, error) {
	var chats []types.TelegramAlertChat
	if err := a.db.WithContext(ctx).
		Where("chat_id = ? AND chat_type = ? AND disabled_at IS NULL AND can_act = ?", telegramUserID, "private", true).
		Limit(1).Find(&chats).Error; err != nil {
		return nil, err
	}
	if len(chats) == 0 {
		return nil, nil
	}
	admins, err := a.superAdmins(ctx)
	if err != nil || !admins[chats[0].LinkedBy] {
		return nil, err
	}
	u, err := a.users.GetUserByID(ctx, chats[0].LinkedBy)
	if err != nil || u == nil {
		return nil, err
	}
	return u, nil
}

// receivesOnly reports whether the Telegram user's private chat is linked
// by an active SuperAdmin with an ordinary code (it receives alerts but may
// not act): the refusal then says how to link it with a personal code.
func (a *TelegramAlerts) receivesOnly(ctx context.Context, telegramUserID int64) bool {
	var chats []types.TelegramAlertChat
	if err := a.db.WithContext(ctx).
		Where("chat_id = ? AND chat_type = ? AND disabled_at IS NULL AND can_act = ?", telegramUserID, "private", false).
		Limit(1).Find(&chats).Error; err != nil || len(chats) == 0 {
		return false
	}
	admins, err := a.superAdmins(ctx)
	return err == nil && admins[chats[0].LinkedBy]
}

func telegramUserName(u *types.User) string {
	if u == nil {
		return ""
	}
	if u.Username != "" {
		return u.Username
	}
	return u.Email
}

// handleCallback serves a press on an order button.
func (a *TelegramAlerts) handleCallback(ctx context.Context, c *telegram.Client, q *telegram.CallbackQuery) {
	answer := func(text string, alert bool) {
		if err := c.AnswerCallback(ctx, q.ID, telegram.CutTextTo(text, 200), alert); err != nil {
			logger.Warnf(ctx, "[TelegramAlerts] answering a button press: %v", err)
		}
	}
	if q.Message == nil {
		answer("", false)
		return
	}
	chatID, msgID := q.Message.Chat.ID, q.Message.MessageID
	act, arg, _ := strings.Cut(q.Data, ":")
	valid := false
	switch act {
	case tgActConfirmAsk, tgActCancelAsk:
		valid = telegramOrderCodeRe.MatchString(arg)
	case tgActConfirm, tgActCancel, tgActDismiss:
		valid = telegramNonceRe.MatchString(arg)
	}
	if !valid {
		answer("", false)
		return
	}
	orders := a.orderService()
	if orders == nil {
		answer("Bot chưa sẵn sàng xử lý đơn hàng.", true)
		return
	}
	if !a.chatLinked(ctx, chatID) {
		answer("Chat này không còn nhận cảnh báo của WeRAG.", true)
		return
	}
	admin, err := a.telegramSuperAdmin(ctx, q.From.ID)
	if err != nil {
		logger.Warnf(ctx, "[TelegramAlerts] who pressed: %v", err)
		answer("Không kiểm tra được quyền, hãy thử lại.", true)
		return
	}
	if admin == nil {
		logger.Warnf(ctx, "[TelegramAlerts] order button %q refused: Telegram user %d in chat %d is not a linked SuperAdmin",
			act, q.From.ID, chatID)
		if a.receivesOnly(ctx, q.From.ID) {
			// a SuperAdmin's chat linked with an ordinary code: say how to fix it
			answer("Telegram này đang liên kết bằng mã thường nên chỉ nhận tin. Để duyệt đơn: Quản trị hệ thống → Thông báo → Telegram → \"Liên kết Telegram của tôi\", rồi bấm Start lại với bot.", true)
			return
		}
		answer("Chỉ SuperAdmin đã liên kết Telegram của chính mình (mã \"Liên kết Telegram của tôi\") mới xử lý được đơn.", true)
		return
	}
	name := telegramUserName(admin)
	if act == tgActConfirmAsk || act == tgActCancelAsk {
		a.askOrder(ctx, c, q, orders, act, arg, name, answer)
		return
	}

	// the second press: the question's nonce, for this presser, chat and message
	key := telegramAskRedisKey + arg
	raw, ok := a.peekEphemeral(ctx, key)
	var ask telegramAsk
	if !ok || json.Unmarshal([]byte(raw), &ask) != nil {
		answer("Câu hỏi này đã hết hạn hoặc đã được trả lời. Hãy bấm lại nút trên tin đơn hàng.", true)
		return
	}
	if ask.User != q.From.ID || ask.Chat != chatID || ask.Msg != msgID {
		answer("Câu hỏi này là của người khác. Hãy bấm nút trên tin đơn hàng để tự xác nhận.", true)
		return
	}
	if _, ok := a.takeEphemeral(ctx, key); !ok { // answered meanwhile
		answer("Câu hỏi này đã được trả lời.", true)
		return
	}
	edit := func(text string) {
		if err := c.EditMessage(ctx, chatID, msgID, text, nil); err != nil {
			logger.Warnf(ctx, "[TelegramAlerts] editing a question: %v", err)
		}
	}
	code := ask.Code
	if act == tgActDismiss {
		edit(fmt.Sprintf("Đã bỏ qua, đơn %s giữ nguyên.", code))
		answer("", false)
		return
	}
	if (act == tgActConfirm) != (ask.Act == tgActConfirmAsk) {
		answer("", false)
		return
	}
	// the question is spent: a failure below says so on it (no dead buttons)
	failed := func(text string) {
		edit(text + " Hãy bấm lại nút trên tin đơn hàng.")
		answer(text, true)
	}
	o, err := orders.GetOrderByCode(ctx, code)
	if err != nil || o == nil || o.UsageOrder == nil {
		failed("Không tìm thấy đơn " + code + ".")
		return
	}
	if o.Status != types.UsageOrderPending {
		state := fmt.Sprintf("Đơn %s %s.", code, telegramOrderStatusVN(o.Status))
		edit(state)
		answer(state, true)
		return
	}
	switch act {
	case tgActConfirm:
		if _, err := orders.ConfirmOrder(ctx, o.ID, admin.ID); err != nil {
			logger.Warnf(ctx, "[TelegramAlerts] confirming order %s for %s: %v", code, admin.ID, err)
			failed("Không xác nhận được đơn " + code + " (lỗi tạm thời hoặc đơn vừa thay đổi).")
			return
		}
		a.auditOrder(ctx, types.AuditActionOrderConfirmedTelegram, admin, o, q)
		edit(fmt.Sprintf("✅ Đơn %s đã được %s xác nhận đã thanh toán lúc %s (trên Telegram).", code, name, formatDateTimeVN(a.now())))
		answer("Đã xác nhận đơn "+code+".", false)
	case tgActCancel:
		if _, err := orders.CancelOrder(ctx, o.ID, false); err != nil {
			logger.Warnf(ctx, "[TelegramAlerts] cancelling order %s for %s: %v", code, admin.ID, err)
			if telegramPaymentReceived(err) {
				msg := "Đơn " + code + " đã nhận tiền nên không hủy được. Hãy xử lý trên trang Thanh toán."
				edit(msg)
				answer(msg, true)
				return
			}
			failed("Không hủy được đơn " + code + " (lỗi tạm thời hoặc đơn vừa thay đổi).")
			return
		}
		a.auditOrder(ctx, types.AuditActionOrderCancelledTelegram, admin, o, q)
		edit(fmt.Sprintf("✖ Đơn %s đã được %s hủy lúc %s (trên Telegram).", code, name, formatDateTimeVN(a.now())))
		answer("Đã hủy đơn "+code+".", false)
	}
}

// askOrder sends the question of a confirm / cancel press, with a one-time
// nonce bound to the presser, the chat and the question message.
func (a *TelegramAlerts) askOrder(ctx context.Context, c *telegram.Client, q *telegram.CallbackQuery,
	orders interfaces.UsageBillingService, act, code, name string, answer func(string, bool),
) {
	chatID := q.Message.Chat.ID
	o, err := orders.GetOrderByCode(ctx, code)
	if err != nil || o == nil || o.UsageOrder == nil {
		answer("Không tìm thấy đơn "+code+".", true)
		return
	}
	if o.Status != types.UsageOrderPending {
		answer(fmt.Sprintf("Đơn %s %s.", code, telegramOrderStatusVN(o.Status)), true)
		return
	}
	nonce, err := newTelegramLinkCode()
	if err != nil {
		answer("Không tạo được câu hỏi xác nhận, hãy thử lại.", true)
		return
	}
	lines := []string{}
	if o.ReceivedVND > 0 {
		lines = append(lines, fmt.Sprintf("Đã nhận qua chuyển khoản: %s / %s", formatVND(o.ReceivedVND), formatVND(o.TotalVND)))
	}
	question, yes, yesAct := fmt.Sprintf("Xác nhận đơn %s đã nhận đủ tiền?", code), "✅ Chắc chắn, đã nhận tiền", tgActConfirm
	if act == tgActCancelAsk {
		question, yes, yesAct = fmt.Sprintf("Hủy đơn %s?", code), "✖ Hủy đơn này", tgActCancel
	}
	text := question + "\n" + telegramOrderLines(o, lines...) + "\nNgười bấm: " + name +
		"\n(Chỉ người bấm trả lời được, trong 10 phút.)"
	msgID, err := c.SendMessage(ctx, chatID, text, telegram.SendOptions{
		ReplyTo: q.Message.MessageID,
		Buttons: [][]telegram.Button{{
			{Text: yes, Data: yesAct + ":" + nonce},
			{Text: "Thôi", Data: tgActDismiss + ":" + nonce},
		}},
	})
	if err != nil {
		logger.Warnf(ctx, "[TelegramAlerts] asking about order %s: %v", code, err)
		answer("Không gửi được câu hỏi xác nhận, hãy thử lại.", true)
		return
	}
	raw, _ := json.Marshal(telegramAsk{Act: act, Code: code, User: q.From.ID, Chat: chatID, Msg: msgID})
	if err := a.putEphemeral(ctx, telegramAskRedisKey+nonce, string(raw), telegramAskTTL); err != nil {
		logger.Warnf(ctx, "[TelegramAlerts] saving the question about order %s: %v", code, err)
		_ = c.EditMessage(ctx, chatID, msgID, "Không lưu được câu hỏi xác nhận đơn "+code+", hãy bấm lại.", nil)
		answer("", false)
		return
	}
	answer("", false)
}

// telegramPaymentReceived: the billing service refused to cancel because the
// order holds money.
func telegramPaymentReceived(err error) bool {
	var app *apperrors.AppError
	if !errors.As(err, &app) {
		return false
	}
	d, ok := app.Details.(map[string]interface{})
	return ok && d["reason"] == "payment_received"
}

func (a *TelegramAlerts) auditOrder(ctx context.Context, action types.AuditAction, admin *types.User,
	o *types.UsageOrderView, q *telegram.CallbackQuery,
) {
	logger.Infof(ctx, "[TelegramAlerts] %s: order %s by %s (telegram user %d)", action, o.OrderCode, admin.ID, q.From.ID)
	a.mu.Lock()
	audit := a.audit
	a.mu.Unlock()
	if audit == nil {
		return
	}
	details, _ := json.Marshal(map[string]any{
		"order_code": o.OrderCode, "total_vnd": o.TotalVND,
		"telegram_user_id": q.From.ID, "chat_id": q.Message.Chat.ID,
	})
	if err := audit.Log(ctx, &types.AuditLog{
		TenantID: types.PlatformTenantID, ActorUserID: admin.ID, ActorRole: "system_admin",
		Action: action, ScopeType: "billing", ScopeID: o.ScopeID,
		TargetType: "usage_order", TargetID: o.ID, Outcome: types.AuditOutcomeSuccess,
		Details: types.JSON(details),
	}); err != nil {
		logger.Warnf(ctx, "[TelegramAlerts] audit of %s: %v", o.OrderCode, err)
	}
}

// CloseOrderButtons takes the buttons of order o off every alert that
// carries them, once the order is no longer pending (paid, cancelled,
// expired — from Telegram, the web page, the payment checker or expiry),
// so nobody presses them again: the order's row is removed (a digest keeps
// the rows of its other orders) and a line tells what happened. Alerts
// still queued lose the row before they are sent. Runs in the background.
func (a *TelegramAlerts) CloseOrderButtons(ctx context.Context, o *types.UsageOrderView) {
	if a == nil || o == nil || o.UsageOrder == nil || o.OrderCode == "" {
		return
	}
	cp := *o
	order := *o.UsageOrder
	cp.UsageOrder = &order
	a.Run(ctx, "closing the buttons of "+order.OrderCode, func(ctx context.Context) error {
		return a.closeOrderButtons(ctx, order.OrderCode, a.telegramOrderState(ctx, &cp))
	})
}

func (a *TelegramAlerts) closeOrderButtons(ctx context.Context, code, state string) error {
	// one close at a time: two orders of one digest decided together must
	// not write over each other's version of the message
	a.closeMu.Lock()
	defer a.closeMu.Unlock()
	var ids []string
	if err := a.db.WithContext(ctx).Model(&types.TelegramAlertMessage{}).
		Where("status IN ? AND CAST(buttons AS TEXT) LIKE ?",
			[]string{types.TelegramMessageSent, types.TelegramMessagePending}, "%\""+tgActConfirmAsk+":"+code+"\"%").
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	token := a.token(ctx)
	var failed []string
	for _, id := range ids {
		if err := a.closeOneMessage(ctx, token, id, code, state); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d alert(s) of %s not updated: %s", len(failed), code, strings.Join(failed, "; "))
	}
	return nil
}

// closeOneMessage re-reads one alert (row-locked on Postgres, so another
// instance's close waits) and removes the order's row from it.
func (a *TelegramAlerts) closeOneMessage(ctx context.Context, token, id, code, state string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("id = ?", id)
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var m types.TelegramAlertMessage
		if err := q.Take(&m).Error; err != nil {
			return err
		}
		var rows [][]telegram.Button
		if json.Unmarshal(m.Buttons, &rows) != nil {
			return nil
		}
		kept := make([][]telegram.Button, 0, len(rows))
		for _, row := range rows {
			if !telegramRowOf(row, code) {
				kept = append(kept, row)
			}
		}
		if len(kept) == len(rows) {
			return nil // closed meanwhile
		}
		text := telegramAppendLine(m.Text, telegram.EscapeHTML("— "+code+": "+state))
		if m.Status == types.TelegramMessageSent && m.MessageID != nil && token != "" {
			if err := a.editWithRetry(ctx, token, m.ChatID, *m.MessageID, text, kept); err != nil {
				return err
			}
		}
		var raw types.JSON
		if len(kept) > 0 {
			b, _ := json.Marshal(kept)
			raw = types.JSON(b)
		}
		return tx.Model(&types.TelegramAlertMessage{}).Where("id = ?", m.ID).
			Updates(map[string]any{"text": text, "buttons": raw}).Error
	})
}

// editWithRetry edits a sent alert; a message Telegram no longer has (or
// already shows so) counts as done, a flood wait is waited out once.
func (a *TelegramAlerts) editWithRetry(ctx context.Context, token string, chatID, msgID int64, text string, rows [][]telegram.Button) error {
	c := a.client(token)
	for attempt := 0; ; attempt++ {
		err := c.EditMessageHTML(ctx, chatID, msgID, text, rows)
		api := telegram.AsAPIError(err)
		switch {
		case err == nil:
			return nil
		case api != nil && (api.ChatGone() || telegramNothingToEdit(api.Description)):
			return nil
		case api != nil && api.RetryAfter > 0 && api.RetryAfter <= 30*time.Second && attempt == 0:
			sleepCtx(ctx, api.RetryAfter+time.Second)
			continue
		}
		return err
	}
}

func telegramNothingToEdit(description string) bool {
	d := strings.ToLower(description)
	return strings.Contains(d, "message is not modified") || strings.Contains(d, "message to edit not found") ||
		strings.Contains(d, "message can't be edited")
}

// telegramAppendLine adds line under text (both HTML), cutting the body
// (not the new line) when the message would be too long.
func telegramAppendLine(text, line string) string {
	room := telegram.MaxTextRunes - len([]rune(line)) - 2
	return telegram.CutHTML(text, room) + "\n" + line
}

// telegramRowOf: the button row belongs to order code.
func telegramRowOf(row []telegram.Button, code string) bool {
	for _, b := range row {
		if b.Data == tgActConfirmAsk+":"+code || b.Data == tgActCancelAsk+":"+code {
			return true
		}
	}
	return false
}

// telegramOrderState is the line an order's alerts get once it is decided:
// "✅ đã thanh toán (an, 15h07 - 26/11/2026)".
func (a *TelegramAlerts) telegramOrderState(ctx context.Context, o *types.UsageOrderView) string {
	at := a.now()
	switch o.Status {
	case types.UsageOrderPaid:
		if o.PaidAt != nil {
			at = *o.PaidAt
		}
		who := "tự khớp chuyển khoản"
		if by := o.ConfirmedBy; by != "" && !strings.HasPrefix(by, paymentConfirmedByPrefix) {
			who = by
			if u, err := a.users.GetUserByID(ctx, by); err == nil && u != nil {
				who = telegramUserName(u)
			}
		}
		return fmt.Sprintf("✅ đã thanh toán (%s, %s)", who, formatDateTimeVN(at))
	case types.UsageOrderCancelled:
		return fmt.Sprintf("✖ đã hủy (%s)", formatDateTimeVN(at))
	case types.UsageOrderExpired:
		return "⌛ quá hạn, đã tự hủy"
	}
	return telegramOrderStatusVN(o.Status)
}
