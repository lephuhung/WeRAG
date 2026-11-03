package types

import (
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// VAT invoices and receipts of usage orders.
//
// A buyer (an organization mostly) may ask for a VAT invoice (hóa đơn
// GTGT) when ordering, or later on the order page: company name, tax code
// (mã số thuế), address and the email that receives the e-invoice. The
// e-invoice is issued in an external system; a SuperAdmin then marks the
// order's invoice issued with its number, series and date. Until then the
// buyer may change the details, also after the payment.
//
// A paid order also has a receipt (biên nhận thanh toán) page: the order
// breakdown with the seller from the billing.seller_* settings. A receipt
// does not replace a VAT invoice.

// UsageOrderInvoice is the invoice part of a usage_orders row (embedded in
// UsageOrder, so its columns sit on the order).
type UsageOrderInvoice struct {
	InvoiceRequested   bool   `json:"invoice_requested"`
	InvoiceCompanyName string `json:"invoice_company_name" gorm:"type:varchar(255)"`
	InvoiceTaxCode     string `json:"invoice_tax_code"     gorm:"type:varchar(14)"`
	InvoiceAddress     string `json:"invoice_address"      gorm:"type:text"`
	InvoiceEmail       string `json:"invoice_email"        gorm:"type:varchar(255)"`
	// InvoiceIssuedAt is when a SuperAdmin marked the invoice issued (nil =
	// not yet); InvoiceNo, InvoiceSeries and InvoiceDate are what the
	// external system gave it (optional).
	InvoiceIssuedAt *time.Time `json:"invoice_issued_at"`
	// InvoiceIssuedBy (a SuperAdmin's user id) is kept from buyer views;
	// SuperAdmin views carry it as UsageOrderView.AdminInvoiceIssuedBy.
	InvoiceIssuedBy string     `json:"-"                  gorm:"type:varchar(36)"`
	InvoiceNo       string     `json:"invoice_no"         gorm:"type:varchar(32)"`
	InvoiceSeries   string     `json:"invoice_series"     gorm:"type:varchar(16)"`
	InvoiceDate     *time.Time `json:"invoice_date"`
}

// InvoiceIssued reports whether a SuperAdmin marked the invoice issued.
func (i *UsageOrderInvoice) InvoiceIssued() bool { return i.InvoiceIssuedAt != nil }

// UsageInvoiceDetails is what the buyer enters for a VAT invoice.
type UsageInvoiceDetails struct {
	CompanyName string `json:"company_name"`
	TaxCode     string `json:"tax_code"`
	Address     string `json:"address"`
	Email       string `json:"email"`
}

// UsageInvoiceUpdate changes an order's invoice request: Requested false
// withdraws it (the details are then cleared).
type UsageInvoiceUpdate struct {
	Requested bool `json:"requested"`
	UsageInvoiceDetails
}

// UsageInvoiceIssue is what a SuperAdmin records when the invoice was
// issued in the external system; every field is optional. Date is
// YYYY-MM-DD (Vietnam calendar), default today.
type UsageInvoiceIssue struct {
	InvoiceNo     string `json:"invoice_no"`
	InvoiceSeries string `json:"invoice_series"`
	Date          string `json:"date"`
}

const (
	usageInvoiceNameMax    = 255
	usageInvoiceAddressMax = 500
	usageInvoiceEmailMax   = 255
	// UsageInvoiceNoMax and UsageInvoiceSeriesMax bound what a SuperAdmin
	// records (the columns' sizes).
	UsageInvoiceNoMax     = 32
	UsageInvoiceSeriesMax = 16
)

var vnTaxCodePattern = regexp.MustCompile(`^[0-9]{10}(-[0-9]{3})?$`)

// NormalizeVNTaxCode removes the spaces people type in a tax code
// ("0101 234 567 - 001").
func NormalizeVNTaxCode(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// ValidVNTaxCode reports whether s (normalized) has the shape of a
// Vietnamese tax code: 10 digits, or 10 digits, "-" and 3 digits (a
// branch / dependent unit).
func ValidVNTaxCode(s string) bool {
	return vnTaxCodePattern.MatchString(s)
}

// collapseSpaces trims and turns every run of whitespace (newlines too)
// into one space.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Normalize cleans the details and checks them; reason is a stable label
// for the UI when they are refused (invoice_company_required,
// invoice_tax_code_invalid, invoice_address_required, invoice_email_invalid,
// invoice_too_long).
func (d UsageInvoiceDetails) Normalize() (UsageInvoiceDetails, string) {
	out := UsageInvoiceDetails{
		CompanyName: collapseSpaces(d.CompanyName),
		TaxCode:     NormalizeVNTaxCode(d.TaxCode),
		Address:     collapseSpaces(d.Address),
		Email:       strings.TrimSpace(d.Email),
	}
	switch {
	case out.CompanyName == "":
		return out, "invoice_company_required"
	case !ValidVNTaxCode(out.TaxCode):
		return out, "invoice_tax_code_invalid"
	case out.Address == "":
		return out, "invoice_address_required"
	case !validInvoiceEmail(out.Email):
		return out, "invoice_email_invalid"
	case utf8.RuneCountInString(out.CompanyName) > usageInvoiceNameMax,
		utf8.RuneCountInString(out.Address) > usageInvoiceAddressMax,
		len(out.Email) > usageInvoiceEmailMax:
		return out, "invoice_too_long"
	}
	return out, ""
}

// validInvoiceEmail accepts a bare address (no display name, no list).
func validInvoiceEmail(s string) bool {
	if s == "" || strings.ContainsAny(s, " <>,;") {
		return false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return false
	}
	at := strings.LastIndex(s, "@")
	return at > 0 && strings.Contains(s[at+1:], ".")
}

// SetInvoiceDetails stores d (already normalized) as requested.
func (i *UsageOrderInvoice) SetInvoiceDetails(d UsageInvoiceDetails) {
	i.InvoiceRequested = true
	i.InvoiceCompanyName = d.CompanyName
	i.InvoiceTaxCode = d.TaxCode
	i.InvoiceAddress = d.Address
	i.InvoiceEmail = d.Email
}

// ClearInvoiceRequest withdraws the request and its details.
func (i *UsageOrderInvoice) ClearInvoiceRequest() {
	i.InvoiceRequested = false
	i.InvoiceCompanyName, i.InvoiceTaxCode, i.InvoiceAddress, i.InvoiceEmail = "", "", "", ""
}

// InvoiceDetails returns the stored details.
func (i *UsageOrderInvoice) InvoiceDetails() UsageInvoiceDetails {
	return UsageInvoiceDetails{
		CompanyName: i.InvoiceCompanyName, TaxCode: i.InvoiceTaxCode,
		Address: i.InvoiceAddress, Email: i.InvoiceEmail,
	}
}

// UsageOrderReceipt is the printable receipt of a paid order.
type UsageOrderReceipt struct {
	Order *UsageOrderView `json:"order"`
	// Seller: from the billing settings (empty fields are left out).
	SellerName    string `json:"seller_name"`
	SellerTaxCode string `json:"seller_tax_code"`
	SellerAddress string `json:"seller_address"`
	SellerContact string `json:"seller_contact"`
	BankName      string `json:"bank_name"`
	BankAccountNo string `json:"bank_account_no"`
	// BuyerName is the organization's name or the person's name;
	// BuyerEmail the person's email on a personal order.
	BuyerName  string `json:"buyer_name"`
	BuyerEmail string `json:"buyer_email,omitempty"`
}

// UsageBillingAdminCounts are the badges of the SuperAdmin billing page.
type UsageBillingAdminCounts struct {
	// AwaitingAdmin: pending orders only a SuperAdmin can move on.
	AwaitingAdmin int64 `json:"awaiting_admin"`
	// InvoicesToIssue: paid orders asking for a VAT invoice not yet issued.
	InvoicesToIssue int64 `json:"invoices_to_issue"`
}
