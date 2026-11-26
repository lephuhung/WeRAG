// Package telegram is the small Bot API client of the system alert bot
// (one bot for the whole platform, configured by a SuperAdmin). It is apart
// from the IM channel adapter (internal/im/telegram), whose bots answer
// questions for one organization.
//
// The token is part of every API URL, and Go's HTTP errors quote the URL,
// so every error this package returns has the token cut out.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the Bot API endpoint.
const DefaultBaseURL = "https://api.telegram.org"

// MaxTextRunes: a message holds 4096 characters; alerts are cut well below.
const MaxTextRunes = 3500

// APIError is a refusal of the Bot API (ok=false).
type APIError struct {
	Method      string
	Code        int
	Description string
	// RetryAfter is set on 429 Too Many Requests.
	RetryAfter time.Duration
	// MigrateToChatID is set when a group became a supergroup: the chat
	// has a new id.
	MigrateToChatID int64
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

// ChatGone reports whether the chat can no longer be written to: the bot
// was blocked, kicked or the chat does not exist (sending again is useless).
func (e *APIError) ChatGone() bool {
	if e == nil {
		return false
	}
	if e.Code == http.StatusForbidden {
		return true
	}
	d := strings.ToLower(e.Description)
	return e.Code == http.StatusBadRequest &&
		(strings.Contains(d, "chat not found") || strings.Contains(d, "group chat was deactivated") ||
			strings.Contains(d, "bot was kicked") || strings.Contains(d, "user is deactivated"))
}

// AsAPIError returns the APIError in err's chain, nil when none.
func AsAPIError(err error) *APIError {
	var e *APIError
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// Client calls the Bot API with one token.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// New returns a client of token; baseURL "" = DefaultBaseURL.
func New(token, baseURL string, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{token: strings.TrimSpace(token), baseURL: strings.TrimRight(baseURL, "/"), http: hc}
}

// User is a Telegram user (the bot itself for GetMe).
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// Chat is the chat of a message.
type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"` // private, group, supergroup, channel
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// Name is how the chat is shown: its title, or the person's name.
func (c Chat) Name() string {
	if c.Title != "" {
		return c.Title
	}
	name := strings.TrimSpace(c.FirstName + " " + c.LastName)
	if c.Username != "" {
		if name == "" {
			return "@" + c.Username
		}
		return name + " (@" + c.Username + ")"
	}
	return name
}

// Message is the part of a message the alert bot reads.
type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from"`
	Text      string `json:"text"`
}

// ChatMemberUpdated reports a change of the bot's membership in a chat.
type ChatMemberUpdated struct {
	Chat          Chat `json:"chat"`
	NewChatMember struct {
		Status string `json:"status"` // member, administrator, left, kicked, …
	} `json:"new_chat_member"`
}

// CallbackQuery is a press on an inline button of a bot message.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// Update is one incoming update.
type Update struct {
	UpdateID      int64              `json:"update_id"`
	Message       *Message           `json:"message"`
	MyChatMember  *ChatMemberUpdated `json:"my_chat_member"`
	CallbackQuery *CallbackQuery     `json:"callback_query"`
}

// Button is an inline button: Data comes back in a CallbackQuery (at most
// 64 bytes), or URL opens a page.
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data,omitempty"`
	URL  string `json:"url,omitempty"`
}

// SendOptions are the options of SendMessage.
type SendOptions struct {
	// Silent delivers without a sound.
	Silent bool
	// Buttons are rows of inline buttons under the message.
	Buttons [][]Button
	// ReplyTo answers that message of the chat (0 = none).
	ReplyTo int64
}

func keyboard(rows [][]Button) map[string]any {
	if rows == nil {
		rows = [][]Button{}
	}
	return map[string]any{"inline_keyboard": rows}
}

// GetMe returns the bot.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var u User
	if err := c.call(ctx, "getMe", map[string]any{}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// SendMessage sends plain text (no parse mode: alert text needs no
// escaping) and returns the message id.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, opt SendOptions) (int64, error) {
	body := map[string]any{
		"chat_id":                  chatID,
		"text":                     CutText(text),
		"disable_web_page_preview": true,
	}
	if opt.Silent {
		body["disable_notification"] = true
	}
	if len(opt.Buttons) > 0 {
		body["reply_markup"] = keyboard(opt.Buttons)
	}
	if opt.ReplyTo != 0 {
		body["reply_parameters"] = map[string]any{"message_id": opt.ReplyTo, "allow_sending_without_reply": true}
	}
	var m Message
	if err := c.call(ctx, "sendMessage", body, &m); err != nil {
		return 0, err
	}
	return m.MessageID, nil
}

// GetUpdates long-polls for updates after offset (timeout in seconds).
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	var ups []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeout,
		"allowed_updates": []string{"message", "my_chat_member", "callback_query"},
	}, &ups)
	return ups, err
}

// EditMessage replaces the text and the buttons of a message (nil buttons
// = none).
func (c *Client) EditMessage(ctx context.Context, chatID, messageID int64, text string, buttons [][]Button) error {
	return c.call(ctx, "editMessageText", map[string]any{
		"chat_id": chatID, "message_id": messageID, "text": CutText(text),
		"disable_web_page_preview": true, "reply_markup": keyboard(buttons),
	}, nil)
}

// AnswerCallback ends the spinner of a button press; text is shown as a
// toast, or as a dialog when alert.
func (c *Client) AnswerCallback(ctx context.Context, id, text string, alert bool) error {
	body := map[string]any{"callback_query_id": id}
	if text != "" {
		body["text"] = text
		body["show_alert"] = alert
	}
	return c.call(ctx, "answerCallbackQuery", body, nil)
}

// IsChatAdmin reports whether userID administers chatID (false on any
// error).
func (c *Client) IsChatAdmin(ctx context.Context, chatID, userID int64) bool {
	var m struct {
		Status string `json:"status"`
	}
	if err := c.call(ctx, "getChatMember", map[string]any{"chat_id": chatID, "user_id": userID}, &m); err != nil {
		return false
	}
	return m.Status == "creator" || m.Status == "administrator"
}

// CutText bounds text to MaxTextRunes characters.
func CutText(text string) string { return CutTextTo(text, MaxTextRunes) }

// CutTextTo bounds text to n characters (a button answer holds 200).
func CutTextTo(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	return string(r[:n-1]) + "…"
}

func (c *Client) call(ctx context.Context, method string, body any, result any) error {
	if c.token == "" {
		return errors.New("telegram: no bot token")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return c.redact(fmt.Errorf("telegram %s: %w", method, err))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return c.redact(fmt.Errorf("telegram %s: %w", method, err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return c.redact(fmt.Errorf("telegram %s: reading the reply: %w", method, err))
	}
	var r struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  struct {
			RetryAfter      int   `json:"retry_after"`
			MigrateToChatID int64 `json:"migrate_to_chat_id"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d, unreadable reply", method, resp.StatusCode)
	}
	if !r.OK {
		code := r.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{
			Method: method, Code: code, Description: c.cut(r.Description),
			RetryAfter:      time.Duration(r.Parameters.RetryAfter) * time.Second,
			MigrateToChatID: r.Parameters.MigrateToChatID,
		}
	}
	if result != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, result); err != nil {
			return fmt.Errorf("telegram %s: decoding the result: %w", method, err)
		}
	}
	return nil
}

// cut removes the token from s.
func (c *Client) cut(s string) string {
	if c.token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.token, "<token>")
}

// redact returns err with the token removed from its text.
func (c *Client) redact(err error) error {
	if err == nil || c.token == "" || !strings.Contains(err.Error(), c.token) {
		return err
	}
	return redactedError{msg: c.cut(err.Error()), err: err}
}

// redactedError keeps the chain (errors.Is on context errors) with a
// token-free text.
type redactedError struct {
	msg string
	err error
}

func (e redactedError) Error() string { return e.msg }
func (e redactedError) Unwrap() error { return e.err }
