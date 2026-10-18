package types

import (
	"strings"
	"time"
)

// Usage packages, billing: promo codes and orders.
//
// A buyer (a Tenant Admin for the organization, anyone for themselves) picks
// a plan, a number of months and, in seat mode, a number of seats, may
// apply a promo code and gets a quote. Creating the order freezes the
// quote; the buyer pays by bank transfer with the order code as transfer
// content (shown as a VietQR code when the bank account is configured).
// SuperAdmin confirms the payment, which creates the subscription or
// extends the current one. Free orders (total 0) confirm themselves.

const (
	UsagePromoPercent = "percent"
	UsagePromoAmount  = "amount"

	UsageOrderPending   = "pending"
	UsageOrderPaid      = "paid"
	UsageOrderCancelled = "cancelled"
	UsageOrderExpired   = "expired"

	UsagePaymentBankTransfer = "bank_transfer"

	// Bank account that receives transfers (VietQR needs the bank BIN and
	// the account number). Billing is "enabled" once the account number
	// is set; until then the purchase page only shows the plans.
	BillingBankBINSettingKey         = "billing.bank_bin"
	BillingBankNameSettingKey        = "billing.bank_name"
	BillingBankAccountNoSettingKey   = "billing.bank_account_no"
	BillingBankAccountNameSettingKey = "billing.bank_account_name"
	// BillingOrderTTLHoursSettingKey: pending orders expire after this.
	BillingOrderTTLHoursSettingKey = "billing.order_ttl_hours"
	// BillingContactSettingKey: free text shown on the payment page.
	BillingContactSettingKey = "billing.contact"
)

// UsagePromoCode is one row of usage_promo_codes.
type UsagePromoCode struct {
	ID            string      `json:"id"             gorm:"primaryKey;type:varchar(36)"`
	Code          string      `json:"code"           gorm:"type:varchar(32);uniqueIndex"`
	Name          string      `json:"name"           gorm:"type:varchar(128)"`
	DiscountType  string      `json:"discount_type"  gorm:"type:varchar(16);default:'percent'"`
	DiscountValue int64       `json:"discount_value"`
	PlanIDs       StringArray `json:"plan_ids"       gorm:"type:jsonb"`
	MaxUses       int         `json:"max_uses"`
	UsedCount     int         `json:"used_count"`
	StartsAt      time.Time   `json:"starts_at"`
	EndsAt        *time.Time  `json:"ends_at"`
	IsActive      bool        `json:"is_active"      gorm:"default:true"`
	Note          string      `json:"note"           gorm:"type:text"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

// TableName returns the table name.
func (UsagePromoCode) TableName() string { return "usage_promo_codes" }

// NormalizeUsagePromoCode is how codes are stored and matched.
func NormalizeUsagePromoCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// AppliesTo reports whether the promo may be used for plan at t; reason
// is a stable label for the UI when it may not.
func (p *UsagePromoCode) AppliesTo(planID string, t time.Time) (ok bool, reason string) {
	switch {
	case p == nil:
		return false, "promo_not_found"
	case !p.IsActive:
		return false, "promo_inactive"
	case !p.StartsAt.IsZero() && t.Before(p.StartsAt):
		return false, "promo_not_started"
	case p.EndsAt != nil && !t.Before(*p.EndsAt):
		return false, "promo_expired"
	case p.MaxUses > 0 && p.UsedCount >= p.MaxUses:
		return false, "promo_exhausted"
	}
	if len(p.PlanIDs) > 0 {
		found := false
		for _, id := range p.PlanIDs {
			if id == planID {
				found = true
				break
			}
		}
		if !found {
			return false, "promo_wrong_plan"
		}
	}
	return true, ""
}

// Discount returns how much the promo takes off subtotal.
func (p *UsagePromoCode) Discount(subtotal int64) int64 {
	if p == nil || subtotal <= 0 {
		return 0
	}
	var d int64
	switch p.DiscountType {
	case UsagePromoPercent:
		d = subtotal * p.DiscountValue / 100
	case UsagePromoAmount:
		d = p.DiscountValue
	}
	if d < 0 {
		d = 0
	}
	if d > subtotal {
		d = subtotal
	}
	return d
}

// UsageOrder is one row of usage_orders: a quote frozen when the buyer
// committed, then paid/cancelled/expired.
type UsageOrder struct {
	ID             string     `json:"id"              gorm:"primaryKey;type:varchar(36)"`
	OrderCode      string     `json:"order_code"      gorm:"type:varchar(24);uniqueIndex"`
	ScopeType      string     `json:"scope_type"      gorm:"type:varchar(16)"`
	ScopeID        string     `json:"scope_id"        gorm:"type:varchar(64)"`
	PlanID         string     `json:"plan_id"         gorm:"type:varchar(36)"`
	PlanName       string     `json:"plan_name"       gorm:"type:varchar(128)"`
	Mode           string     `json:"mode"            gorm:"type:varchar(16);default:'pool'"`
	Seats          int        `json:"seats"`
	Months         int        `json:"months"`
	UnitPriceVND   int64      `json:"unit_price_vnd"`
	SubtotalVND    int64      `json:"subtotal_vnd"`
	PromoCodeID    *string    `json:"promo_code_id"   gorm:"type:varchar(36)"`
	PromoCode      string     `json:"promo_code"      gorm:"type:varchar(32)"`
	DiscountVND    int64      `json:"discount_vnd"`
	TotalVND       int64      `json:"total_vnd"`
	Status         string     `json:"status"          gorm:"type:varchar(16);default:'pending'"`
	PaymentMethod  string     `json:"payment_method"  gorm:"type:varchar(32);default:'bank_transfer'"`
	CreatedBy      string     `json:"created_by"      gorm:"type:varchar(36)"`
	ConfirmedBy    string     `json:"confirmed_by"    gorm:"type:varchar(36)"`
	PaidAt         *time.Time `json:"paid_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	SubscriptionID *string    `json:"subscription_id" gorm:"type:varchar(36)"`
	Note           string     `json:"note"            gorm:"type:text"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TableName returns the table name.
func (UsageOrder) TableName() string { return "usage_orders" }

// UsageQuoteRequest is what the buyer asks for (quote and order share it).
type UsageQuoteRequest struct {
	ScopeType string `json:"scope_type"`
	PlanID    string `json:"plan_id"`
	Months    int    `json:"months"`
	Seats     int    `json:"seats"`
	PromoCode string `json:"promo_code"`
	Note      string `json:"note"`
}

// UsageQuote prices a request. PromoError is set (and no discount applied)
// when the code was given but cannot be used, so the page can say why
// without failing the whole quote.
type UsageQuote struct {
	Plan         *UsagePlan `json:"plan"`
	Mode         string     `json:"mode"`
	Months       int        `json:"months"`
	Seats        int        `json:"seats"`
	UnitPriceVND int64      `json:"unit_price_vnd"`
	// CurrentSeats is the seat count of the subscription this order renews
	// (0 when it starts a new one). Added seats also pay ProrationVND: the
	// added seats for the ProratedDays left on the current period.
	CurrentSeats int    `json:"current_seats,omitempty"`
	ProratedDays int    `json:"prorated_days,omitempty"`
	ProrationVND int64  `json:"proration_vnd,omitempty"`
	SubtotalVND  int64  `json:"subtotal_vnd"`
	PromoCode    string `json:"promo_code"`
	PromoName    string `json:"promo_name"`
	PromoError   string `json:"promo_error"`
	DiscountVND  int64  `json:"discount_vnd"`
	TotalVND     int64  `json:"total_vnd"`
}

// UsageBillingInfo is what the payment page needs besides the order.
type UsageBillingInfo struct {
	// Enabled: a receiving account is configured, so orders can be paid.
	Enabled         bool   `json:"enabled"`
	BankBIN         string `json:"bank_bin"`
	BankName        string `json:"bank_name"`
	BankAccountNo   string `json:"bank_account_no"`
	BankAccountName string `json:"bank_account_name"`
	OrderTTLHours   int64  `json:"order_ttl_hours"`
	Contact         string `json:"contact"`
}

// UsageOrderFilter narrows an order listing.
type UsageOrderFilter struct {
	ScopeType string
	ScopeID   string
	Status    string
	CreatedBy string
	Limit     int
}

// UsageOrderView is an order as pages show it: with the name of what it
// buys for and the ready-made VietQR image URL when billing is enabled.
type UsageOrderView struct {
	*UsageOrder
	ScopeName string `json:"scope_name"`
	// TransferContent is what the buyer must write in the transfer so the
	// payment can be matched to the order.
	TransferContent string `json:"transfer_content"`
	QRURL           string `json:"qr_url"`
}
