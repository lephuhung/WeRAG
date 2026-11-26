package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tencent/WeKnora/internal/alerting/telegram"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// buttonOrders is the billing service the order buttons act through.
type buttonOrders struct {
	interfaces.UsageBillingService
	orders     map[string]*types.UsageOrderView // by code
	confirmed  []string                         // "id by"
	cancelled  []string
	cancelErr  error
	confirmErr error
}

func (b *buttonOrders) GetOrderByCode(_ context.Context, code string) (*types.UsageOrderView, error) {
	if o, ok := b.orders[code]; ok {
		return o, nil
	}
	return nil, errors.New("order not found")
}

func (b *buttonOrders) find(id string) *types.UsageOrderView {
	for _, o := range b.orders {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func (b *buttonOrders) ConfirmOrder(_ context.Context, id, by string) (*types.UsageOrderView, error) {
	if b.confirmErr != nil {
		return nil, b.confirmErr
	}
	o := b.find(id)
	if o.Status != types.UsageOrderPending {
		return nil, errors.New("order is not pending")
	}
	o.Status = types.UsageOrderPaid
	b.confirmed = append(b.confirmed, id+" "+by)
	return o, nil
}

func (b *buttonOrders) CancelOrder(_ context.Context, id string, _ bool) (*types.UsageOrderView, error) {
	if b.cancelErr != nil {
		return nil, b.cancelErr
	}
	o := b.find(id)
	o.Status = types.UsageOrderCancelled
	b.cancelled = append(b.cancelled, id)
	return o, nil
}

type recordingAudit struct {
	interfaces.AuditLogService
	rows []*types.AuditLog
}

func (r *recordingAudit) Log(_ context.Context, e *types.AuditLog) error {
	r.rows = append(r.rows, e)
	return nil
}

func (b *fakeBot) callsOf(method string) []fakeBotCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []fakeBotCall
	for _, c := range b.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func pressOnMsg(chat, from int64, data string, msg int64) telegram.Update {
	u := pressOn(chat, "group", from, data)
	u.CallbackQuery.Message.MessageID = msg
	return u
}

func pressOn(chat int64, chatType string, from int64, data string) telegram.Update {
	return telegram.Update{UpdateID: time.Now().UnixNano(), CallbackQuery: &telegram.CallbackQuery{
		ID: "cb", From: telegram.User{ID: from}, Data: data,
		Message: &telegram.Message{MessageID: 42, Chat: telegram.Chat{ID: chat, Type: chatType}},
	}}
}

// nonceOf reads the nonce of the latest question's "yes" (0) or "Thôi" (1).
func nonceOf(t *testing.T, b *fakeBot, which int) string {
	t.Helper()
	sent := b.sent()
	rows := sent[len(sent)-1].Body["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	data := rows[0].([]any)[which].(map[string]any)["callback_data"].(string)
	_, nonce, _ := strings.Cut(data, ":")
	return nonce
}

func newButtonEnv(t *testing.T) (*telegramEnv, *buttonOrders, *recordingAudit) {
	e := newTelegramEnv(t)
	order := func(id, code string) *types.UsageOrderView {
		return &types.UsageOrderView{UsageOrder: &types.UsageOrder{ID: id, OrderCode: code, PlanName: "Cơ bản",
			ScopeType: types.UsageScopeTenant, ScopeID: "10003", TotalVND: 49000, Status: types.UsageOrderPending}, ScopeName: "Sở Y tế"}
	}
	orders := &buttonOrders{orders: map[string]*types.UsageOrderView{"WRAA000001": order("o1", "WRAA000001"), "WRBB000002": order("o2", "WRBB000002")}}
	audit := &recordingAudit{}
	e.alerts.SetOrderActions(orders, audit)
	// admin1's own Telegram (user 1001, personal code: can act) and a group
	e.link(t, 1001, "admin1")
	require.NoError(t, e.db.Model(&types.TelegramAlertChat{}).Where("chat_id = ?", 1001).Update("can_act", true).Error)
	require.NoError(t, e.db.Create(&types.TelegramAlertChat{ID: "g", ChatID: -500, ChatType: "group", LinkedBy: "admin1", LinkedAt: time.Now()}).Error)
	return e, orders, audit
}

func TestTelegramOrderButtonsOnNewOrdersDigest(t *testing.T) {
	e, _, _ := newButtonEnv(t)
	ctx := context.Background()
	payload, _ := json.Marshal(types.UserNotificationQueuePayload{Count: 2, Items: []types.UserNotificationQueueItem{
		{Ref: "WRAA000001", Plan: "Cơ bản", AmountVND: 49000}, {Ref: "WRBB000002", Plan: "Cơ bản", AmountVND: 49000},
	}})
	e.alerts.sendDigest(ctx, types.UserNotification{Kind: types.UserNotificationPaymentOrderPending,
		Title: "Có 2 đơn hàng mới chờ xác nhận thanh toán", Payload: types.JSON(payload)})
	_, err := e.alerts.SendBatch(ctx, e.client)
	require.NoError(t, err)
	sent := e.bot.sent()
	require.Len(t, sent, 2) // the private chat and the group
	rows := sent[0].Body["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	require.Len(t, rows, 2)
	first := rows[0].([]any)
	require.Equal(t, "✅ Đã nhận tiền WRAA000001", first[0].(map[string]any)["text"])
	require.Equal(t, "oc:WRAA000001", first[0].(map[string]any)["callback_data"])
	require.Equal(t, "ox:WRAA000001", first[1].(map[string]any)["callback_data"])

	// another kind carries no buttons
	e.alerts.sendDigest(ctx, types.UserNotification{Kind: types.UserNotificationLegalDraftReady, Title: "Có 1 bản nháp"})
	_, err = e.alerts.SendBatch(ctx, e.client)
	require.NoError(t, err)
	require.Nil(t, e.bot.sent()[2].Body["reply_markup"])
}

func TestTelegramConfirmOrderFromGroup(t *testing.T) {
	e, orders, audit := newButtonEnv(t)
	ctx := context.Background()
	lastAnswer := func() map[string]any {
		calls := e.bot.callsOf("answerCallbackQuery")
		return calls[len(calls)-1].Body
	}

	// a group member who did not link their own Telegram is refused
	e.bot.push(pressOn(-500, "group", 777, "oc:WRAA000001"))
	_, err := e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Contains(t, lastAnswer()["text"], "Chỉ SuperAdmin đã liên kết Telegram của chính mình")
	require.Equal(t, true, lastAnswer()["show_alert"])
	require.Empty(t, e.bot.sent())

	// admin1 (Telegram user 1001) presses in the group: asked again first
	e.bot.push(pressOn(-500, "group", 1001, "oc:WRAA000001"))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	ask := e.bot.sent()
	require.Len(t, ask, 1)
	require.Equal(t, float64(-500), ask[0].Body["chat_id"])
	require.Contains(t, ask[0].Body["text"], "Xác nhận đơn WRAA000001 đã nhận đủ tiền?")
	require.Contains(t, ask[0].Body["text"], "Người bấm: an")
	require.Equal(t, float64(42), ask[0].Body["reply_parameters"].(map[string]any)["message_id"])
	require.Empty(t, orders.confirmed, "nothing done before the second press")

	nonce := nonceOf(t, e.bot, 0)
	questionMsg := e.bot.nextID // the fake bot numbers messages

	// another SuperAdmin's press on that question is refused (not theirs),
	// as is "ocy:" sent without a question, or onto another message
	e.bot.push(pressOnMsg(-500, 1001, "ocy:"+nonce, 42))
	e.bot.push(pressOnMsg(-500, 1001, "ocy:ZZZZZZZZZZ", questionMsg))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Empty(t, orders.confirmed)

	// "Chắc chắn": confirmed as admin1, the question edited, audited
	e.bot.push(pressOnMsg(-500, 1001, "ocy:"+nonce, questionMsg))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"o1 admin1"}, orders.confirmed)
	edits := e.bot.callsOf("editMessageText")
	require.Contains(t, edits[len(edits)-1].Body["text"], "✅ Đơn WRAA000001 đã được an xác nhận đã thanh toán")
	require.Len(t, audit.rows, 1)
	require.Equal(t, types.AuditActionOrderConfirmedTelegram, audit.rows[0].Action)
	require.Equal(t, "admin1", audit.rows[0].ActorUserID)
	require.Equal(t, "o1", audit.rows[0].TargetID)

	// a double tap: the question is spent
	e.bot.push(pressOnMsg(-500, 1001, "ocy:"+nonce, questionMsg))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Len(t, orders.confirmed, 1)
	require.Contains(t, lastAnswer()["text"], "đã hết hạn hoặc đã được trả lời")
	// asking again about the paid order: told it is paid
	e.bot.push(pressOn(-500, "group", 1001, "oc:WRAA000001"))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Equal(t, "Đơn WRAA000001 đã thanh toán.", lastAnswer()["text"])
}

func TestTelegramCancelAndDismiss(t *testing.T) {
	e, orders, audit := newButtonEnv(t)
	ctx := context.Background()
	lastEdit := func() string {
		edits := e.bot.callsOf("editMessageText")
		return edits[len(edits)-1].Body["text"].(string)
	}
	lastAnswer := func() string {
		answers := e.bot.callsOf("answerCallbackQuery")
		t, _ := answers[len(answers)-1].Body["text"].(string)
		return t
	}
	ask := func(data string) (string, string, int64) {
		e.bot.push(pressOn(1001, "private", 1001, data))
		_, err := e.alerts.PollOnce(ctx, e.client, 0)
		require.NoError(t, err)
		return nonceOf(t, e.bot, 0), nonceOf(t, e.bot, 1), e.bot.nextID
	}
	press := func(data string, msg int64) {
		u := pressOn(1001, "private", 1001, data)
		u.CallbackQuery.Message.MessageID = msg
		e.bot.push(u)
		_, err := e.alerts.PollOnce(ctx, e.client, 0)
		require.NoError(t, err)
	}

	// "Thôi" leaves the order as it is (and spends the question)
	_, no, q := ask("ox:WRBB000002")
	press("no:"+no, q)
	require.Equal(t, "Đã bỏ qua, đơn WRBB000002 giữ nguyên.", lastEdit())

	// a refused cancel (money held) says so and does nothing
	orders.cancelErr = apperrors.NewConflictError("held").WithDetails(map[string]interface{}{"reason": "payment_received"})
	yes, _, q := ask("ox:WRBB000002")
	press("oxy:"+yes, q)
	require.Contains(t, lastAnswer(), "đã nhận tiền nên không hủy được")
	require.Empty(t, audit.rows)

	orders.cancelErr = nil
	yes, _, q = ask("ox:WRBB000002")
	press("oxy:"+yes, q)
	require.Equal(t, []string{"o2"}, orders.cancelled)
	require.True(t, strings.HasPrefix(lastEdit(), "✖ Đơn WRBB000002 đã được an hủy lúc"))
	require.Equal(t, types.AuditActionOrderCancelledTelegram, audit.rows[0].Action)

	// a confirm question cannot be answered with "cancel"
	yes, _, q = ask("oc:WRAA000001")
	press("oxy:"+yes, q)
	require.Empty(t, orders.confirmed)
	require.Equal(t, types.UsageOrderPending, orders.orders["WRAA000001"].Status)

	// a press from a chat that no longer gets alerts is refused
	e.bot.push(pressOn(-999, "group", 1001, "oc:WRAA000001"))
	_, err := e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Contains(t, lastAnswer(), "không còn nhận cảnh báo")

	// "Thôi" from someone not allowed does nothing
	e.bot.push(pressOn(-500, "group", 777, "no:"+yes))
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Contains(t, lastAnswer(), "Chỉ SuperAdmin đã liên kết Telegram của chính mình")
}

// Only a chat linked with a personal code acts: a private chat linked with
// an ordinary code (a colleague who receives alerts) is refused, and a
// personal code does not link a group.
func TestTelegramPersonalLinkOnly(t *testing.T) {
	e, orders, _ := newButtonEnv(t)
	ctx := context.Background()
	e.link(t, 2002, "admin1") // ordinary code: can_act false
	e.bot.push(pressOn(2002, "private", 2002, "oc:WRAA000001"))
	_, err := e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	answers := e.bot.callsOf("answerCallbackQuery")
	require.Contains(t, answers[len(answers)-1].Body["text"], "đang liên kết bằng mã thường nên chỉ nhận tin")
	require.Empty(t, orders.confirmed)

	code, err := e.alerts.CreateLinkCode(ctx, "admin2", true)
	require.NoError(t, err)
	require.True(t, code.Personal)
	require.Empty(t, code.GroupLink)
	group := telegram.Update{UpdateID: 1, Message: &telegram.Message{Chat: telegram.Chat{ID: -600, Type: "group"},
		From: &telegram.User{ID: 3003}, Text: "/start@werag_alert_bot " + code.Code}}
	e.bot.push(group)
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	var n int64
	require.NoError(t, e.db.Model(&types.TelegramAlertChat{}).Where("chat_id = ?", -600).Count(&n).Error)
	require.Zero(t, n, "a personal code links no group")

	code, _ = e.alerts.CreateLinkCode(ctx, "admin2", true)
	e.bot.push(telegram.Update{UpdateID: 2, Message: &telegram.Message{Chat: telegram.Chat{ID: 3003, Type: "private"},
		From: &telegram.User{ID: 3003}, Text: "/start " + code.Code}})
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	var c types.TelegramAlertChat
	require.NoError(t, e.db.Where("chat_id = ?", 3003).Take(&c).Error)
	require.True(t, c.CanAct)
	require.Equal(t, "admin2", c.LinkedBy)
}

// A SuperAdmin who lost the role cannot act, even from their own chat.
func TestTelegramOrderButtonsNeedActiveSuperAdmin(t *testing.T) {
	e, orders, _ := newButtonEnv(t)
	ctx := context.Background()
	e.users.users["admin1"].IsSystemAdmin = false
	// keep the chat receiving: linked by admin2 now, but the presser is still 1001 → admin2
	require.NoError(t, e.db.Model(&types.TelegramAlertChat{}).Where("chat_id = ?", -500).Update("linked_by", "admin2").Error)
	e.bot.push(pressOn(-500, "group", 1001, "oc:WRAA000001"))
	_, err := e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	require.Empty(t, e.bot.sent(), "no question for a former SuperAdmin")
	require.Empty(t, orders.confirmed)
}

// A failed confirm after the question is spent says so on the question and
// removes its buttons (no dead "Chắc chắn").
func TestTelegramFailedConfirmClosesQuestion(t *testing.T) {
	e, orders, audit := newButtonEnv(t)
	ctx := context.Background()
	orders.confirmErr = errors.New("database is down")
	e.bot.push(pressOn(1001, "private", 1001, "oc:WRAA000001"))
	_, err := e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	nonce, q := nonceOf(t, e.bot, 0), e.bot.nextID
	u := pressOn(1001, "private", 1001, "ocy:"+nonce)
	u.CallbackQuery.Message.MessageID = q
	e.bot.push(u)
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	edits := e.bot.callsOf("editMessageText")
	last := edits[len(edits)-1].Body
	require.Contains(t, last["text"], "Không xác nhận được đơn WRAA000001")
	require.Contains(t, last["text"], "Hãy bấm lại nút trên tin đơn hàng.")
	require.Empty(t, last["reply_markup"].(map[string]any)["inline_keyboard"])
	require.NotContains(t, last["text"], "database", "no internals in the chat")
	require.Empty(t, audit.rows)
}

// Re-linking one's own chat with an ordinary code keeps the right to act.
func TestTelegramRelinkKeepsCanAct(t *testing.T) {
	e, _, _ := newButtonEnv(t)
	ctx := context.Background()
	code, err := e.alerts.CreateLinkCode(ctx, "admin1", false)
	require.NoError(t, err)
	e.bot.push(telegram.Update{UpdateID: 5, Message: &telegram.Message{Chat: telegram.Chat{ID: 1001, Type: "private"},
		From: &telegram.User{ID: 1001}, Text: "/start " + code.Code}})
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	var c types.TelegramAlertChat
	require.NoError(t, e.db.Where("chat_id = ?", 1001).Take(&c).Error)
	require.True(t, c.CanAct)
	// another SuperAdmin's ordinary code on that chat does not keep it
	code, _ = e.alerts.CreateLinkCode(ctx, "admin2", false)
	e.bot.push(telegram.Update{UpdateID: 6, Message: &telegram.Message{Chat: telegram.Chat{ID: 1001, Type: "private"},
		From: &telegram.User{ID: 1001}, Text: "/start " + code.Code}})
	_, err = e.alerts.PollOnce(ctx, e.client, 0)
	require.NoError(t, err)
	var c2 types.TelegramAlertChat
	require.NoError(t, e.db.Where("chat_id = ?", 1001).Take(&c2).Error)
	require.False(t, c2.CanAct)
}
