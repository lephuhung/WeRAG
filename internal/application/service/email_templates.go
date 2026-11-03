package service

import (
	"bytes"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/mailer"
	"github.com/Tencent/WeKnora/internal/types"
)

// Vietnamese email templates (the product's users are Vietnamese; no
// English variant). Each mail has a plain-text body and a simple HTML one
// built from the same lines.

// emailLine is one "label: value" row of a mail.
type emailLine struct {
	Label string
	Value string
}

// emailContent is what a template renders.
type emailContent struct {
	Subject  string
	Greeting string
	Intro    string
	Lines    []emailLine
	Outro    string
	LinkText string
	Link     string
}

var emailHTMLTemplate = template.Must(template.New("mail").Parse(`<!DOCTYPE html>
<html lang="vi"><head><meta charset="utf-8"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:24px;background:#f6f6f4;font-family:Arial,Helvetica,sans-serif;color:#1f1f1f;font-size:14px;line-height:1.6">
<div style="max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #e6e6e2;border-radius:8px;padding:24px">
{{if .Greeting}}<p style="margin:0 0 12px">{{.Greeting}}</p>{{end}}
<p style="margin:0 0 16px">{{.Intro}}</p>
{{if .Lines}}<table style="border-collapse:collapse;width:100%;margin:0 0 16px">{{range .Lines}}
<tr><td style="padding:6px 12px 6px 0;color:#6b6b66;white-space:nowrap;vertical-align:top">{{.Label}}</td><td style="padding:6px 0;vertical-align:top"><strong>{{.Value}}</strong></td></tr>{{end}}
</table>{{end}}
{{if .Outro}}<p style="margin:0 0 16px">{{.Outro}}</p>{{end}}
{{if .Link}}<p style="margin:0 0 16px"><a href="{{.Link}}" style="display:inline-block;background:#1f1f1f;color:#ffffff;text-decoration:none;padding:10px 18px;border-radius:6px">{{.LinkText}}</a></p>
<p style="margin:0;color:#6b6b66;font-size:12px">{{.Link}}</p>{{end}}
</div>
<p style="max-width:560px;margin:12px auto 0;color:#9a9a94;font-size:12px">Email này được gửi tự động, vui lòng không trả lời.</p>
</body></html>`))

// render builds the mail of c for recipients to.
func (c emailContent) render(to []string) (mailer.Message, error) {
	var text strings.Builder
	if c.Greeting != "" {
		text.WriteString(c.Greeting + "\n\n")
	}
	text.WriteString(c.Intro + "\n\n")
	for _, l := range c.Lines {
		text.WriteString("- " + l.Label + ": " + l.Value + "\n")
	}
	if len(c.Lines) > 0 {
		text.WriteString("\n")
	}
	if c.Outro != "" {
		text.WriteString(c.Outro + "\n\n")
	}
	if c.Link != "" {
		text.WriteString(c.LinkText + ": " + c.Link + "\n\n")
	}
	text.WriteString("Email này được gửi tự động, vui lòng không trả lời.\n")
	var html bytes.Buffer
	if err := emailHTMLTemplate.Execute(&html, c); err != nil {
		return mailer.Message{}, err
	}
	return mailer.Message{To: to, Subject: c.Subject, Text: text.String(), HTML: html.String()}, nil
}

// formatVND writes 1250000 as "1.250.000 đ".
func formatVND(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	out := b.String() + " đ"
	if neg {
		out = "-" + out
	}
	return out
}

// formatDateVN is dd/mm/yyyy in Vietnam time.
func formatDateVN(t time.Time) string {
	return t.In(types.UsageLocation()).Format("02/01/2006")
}

// formatDateTimeVN is "14h05 - 16/10/2026" in Vietnam time.
func formatDateTimeVN(t time.Time) string {
	return t.In(types.UsageLocation()).Format("15h04 - 02/01/2006")
}

// orderScopeLabel says who the order is for.
func orderScopeLabel(o *types.UsageOrderView) string {
	name := strings.TrimSpace(o.ScopeName)
	if name == "" {
		name = o.ScopeID
	}
	if o.ScopeType == types.UsageScopeTenant {
		return "Tổ chức " + name
	}
	return "Cá nhân " + name
}

// orderWhatLabel is the plan line of an order: plan, seats, months and the
// add-ons ("Trợ lý chuyên ngành").
func orderWhatLabel(o *types.UsageOrderView) string {
	var parts []string
	if o.Kind == types.UsageOrderKindSeats {
		p := fmt.Sprintf("%s · thêm %d chỗ đến hết kỳ hiện tại", o.PlanName, o.Seats)
		// the add-ons are already held: the order only pays them for the new seats
		if len(o.Addons) > 0 {
			names := make([]string, 0, len(o.Addons))
			for _, a := range o.Addons {
				n := a.Name
				if n == "" {
					n = a.AgentID
				}
				names = append(names, n)
			}
			p += " · Trợ lý chuyên ngành tính thêm cho chỗ mới: " + strings.Join(names, ", ")
		}
		return p
	} else if o.Kind != types.UsageOrderKindAddon {
		p := o.PlanName
		if o.Months > 0 {
			p += fmt.Sprintf(" · %d tháng", o.Months)
		}
		if o.Mode == types.UsageModeSeat && o.Seats > 0 {
			p += fmt.Sprintf(" · %d chỗ", o.Seats)
		}
		parts = append(parts, p)
	}
	if len(o.Addons) > 0 {
		names := make([]string, 0, len(o.Addons))
		for _, a := range o.Addons {
			n := a.Name
			if n == "" {
				n = a.AgentID
			}
			names = append(names, n)
		}
		parts = append(parts, "Trợ lý chuyên ngành: "+strings.Join(names, ", "))
	}
	return strings.Join(parts, " · ")
}

// orderCreatedEmail tells the SuperAdmins a new order waits for payment.
func orderCreatedEmail(o *types.UsageOrderView, adminLink string) emailContent {
	lines := []emailLine{
		{"Mã đơn", o.OrderCode},
		{"Mua cho", orderScopeLabel(o)},
		{"Gói", orderWhatLabel(o)},
		{"Số tiền", formatVND(o.TotalVND)},
		{"Tạo lúc", formatDateTimeVN(o.CreatedAt)},
	}
	if o.PromoCode != "" {
		lines = append(lines, emailLine{"Mã khuyến mãi", o.PromoCode})
	}
	if o.ExpiresAt != nil {
		lines = append(lines, emailLine{"Hết hạn thanh toán", formatDateTimeVN(*o.ExpiresAt)})
	}
	outro := "Hãy xác nhận đơn khi đã nhận được tiền chuyển khoản."
	if o.TotalVND <= 0 {
		outro = "Đơn 0 đồng không cần chuyển khoản; hãy xác nhận đơn để kích hoạt gói."
	}
	return emailContent{
		Subject:  fmt.Sprintf("Đơn hàng mới %s chờ xác nhận (%s)", o.OrderCode, formatVND(o.TotalVND)),
		Intro:    "Có đơn hàng mới đang chờ thanh toán.",
		Lines:    lines,
		Outro:    outro,
		LinkText: "Mở trang thanh toán",
		Link:     adminLink,
	}
}

// orderPaidEmail tells the buyer the order was confirmed; endsAt is the new
// package end (stored as the next Vietnam midnight), nil when unknown.
func orderPaidEmail(o *types.UsageOrderView, endsAt *time.Time, buyLink string) emailContent {
	lines := []emailLine{
		{"Mã đơn", o.OrderCode},
		{"Mua cho", orderScopeLabel(o)},
		{"Gói", orderWhatLabel(o)},
		{"Số tiền", formatVND(o.TotalVND)},
	}
	if o.PaidAt != nil {
		lines = append(lines, emailLine{"Xác nhận lúc", formatDateTimeVN(*o.PaidAt)})
	}
	if endsAt != nil {
		lines = append(lines, emailLine{"Thời hạn", "dùng được đến hết ngày " + formatDateVN(types.LastUsableDayVN(*endsAt))})
	}
	return emailContent{
		Subject:  fmt.Sprintf("Đơn hàng %s đã được xác nhận", o.OrderCode),
		Intro:    "Thanh toán của bạn đã được xác nhận và gói đã được kích hoạt.",
		Lines:    lines,
		Outro:    "Cảm ơn bạn đã sử dụng dịch vụ.",
		LinkText: "Xem gói đang dùng",
		Link:     buyLink,
	}
}

// expiryReminderEmail reminds that a package ends soon.
func expiryReminderEmail(planName, scopeLabel string, endsAt time.Time, daysLeft int, buyLink string) emailContent {
	last := formatDateVN(types.LastUsableDayVN(endsAt))
	when := fmt.Sprintf("còn %d ngày", daysLeft)
	if daysLeft == 0 {
		when = "hôm nay là ngày cuối"
	}
	return emailContent{
		Subject: fmt.Sprintf("Gói %s sắp hết hạn (%s)", planName, when),
		Intro: fmt.Sprintf("Gói %s của %s dùng được đến hết ngày %s (%s). Hãy gia hạn để không bị gián đoạn.",
			planName, scopeLabel, last, when),
		Lines: []emailLine{
			{"Gói", planName},
			{"Dùng cho", scopeLabel},
			{"Dùng được đến hết ngày", last},
		},
		LinkText: "Mua hoặc gia hạn gói",
		Link:     buyLink,
	}
}

// testEmail is the "Gửi thử" mail.
func testEmail(now time.Time) emailContent {
	return emailContent{
		Subject: "Email thử từ hệ thống",
		Intro:   "Cấu hình SMTP hoạt động: email này được gửi lúc " + formatDateTimeVN(now) + ".",
	}
}
