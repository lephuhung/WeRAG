package types

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// TenantAgencyProfile is the issuing agency of an organization (one đơn vị), as
// the header and signature block of its documents need it. A document drafted
// from chat (prepare_document_template) takes these values from here, never
// from the model; the organization's admin or a SuperAdmin edits them.
type TenantAgencyProfile struct {
	TenantID uint64 `json:"tenant_id" gorm:"primaryKey"`
	// ParentAgency is the cơ quan chủ quản ("UBND TỈNH THỪA THIÊN HUẾ"),
	// empty when the agency has none.
	ParentAgency string `json:"parent_agency"`
	// AgencyName is the cơ quan ban hành ("SỞ NỘI VỤ").
	AgencyName string `json:"agency_name"`
	// AgencyAbbr is its abbreviation in số ký hiệu ("SNV").
	AgencyAbbr string `json:"agency_abbr"`
	// Locality is the địa danh of "<địa danh>, ngày … tháng … năm …".
	Locality string `json:"locality"`
	// HeadTitle is the chức vụ of the head, written after KT./TL./TUQ./Q.
	HeadTitle string `json:"head_title"`
	// CollectiveName is the leadership collective written after TM., empty
	// when the agency does not work collectively.
	CollectiveName string `json:"collective_name"`
	// DefaultRecipients are the usual "Nơi nhận" lines.
	DefaultRecipients StringArray `json:"default_recipients" gorm:"type:jsonb"`
	Version           int         `json:"version"`
	UpdatedBy         string      `json:"updated_by"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

// TableName names the table of TenantAgencyProfile.
func (TenantAgencyProfile) TableName() string { return "tenant_agency_profiles" }

// Agency profile limits.
const (
	AgencyProfileTextMax       = 255
	AgencyProfileAbbrMax       = 32
	AgencyProfileLocalityMax   = 128
	AgencyProfileRecipientsMax = 30
)

// ErrAgencyProfileInvalid wraps every validation failure of a profile.
var ErrAgencyProfileInvalid = errors.New("invalid agency profile")

// Normalize trims the fields and drops empty recipient lines.
func (p *TenantAgencyProfile) Normalize() {
	for _, f := range []*string{&p.ParentAgency, &p.AgencyName, &p.AgencyAbbr, &p.Locality, &p.HeadTitle, &p.CollectiveName} {
		*f = strings.Join(strings.Fields(*f), " ")
	}
	lines := StringArray{}
	for _, l := range p.DefaultRecipients {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	p.DefaultRecipients = lines
}

// Validate checks a normalized profile: the agency name is required and
// every field fits its column.
func (p *TenantAgencyProfile) Validate() error {
	if p.AgencyName == "" {
		return fmt.Errorf("%w: agency_name is required", ErrAgencyProfileInvalid)
	}
	for name, v := range map[string]string{"parent_agency": p.ParentAgency, "agency_name": p.AgencyName,
		"head_title": p.HeadTitle, "collective_name": p.CollectiveName} {
		if utf8.RuneCountInString(v) > AgencyProfileTextMax {
			return fmt.Errorf("%w: %s is longer than %d characters", ErrAgencyProfileInvalid, name, AgencyProfileTextMax)
		}
	}
	if utf8.RuneCountInString(p.AgencyAbbr) > AgencyProfileAbbrMax || strings.ContainsAny(p.AgencyAbbr, " /") {
		return fmt.Errorf("%w: agency_abbr is at most %d characters, without spaces or '/'", ErrAgencyProfileInvalid, AgencyProfileAbbrMax)
	}
	if utf8.RuneCountInString(p.Locality) > AgencyProfileLocalityMax || strings.Contains(p.Locality, ",") {
		return fmt.Errorf("%w: locality is at most %d characters, without a comma", ErrAgencyProfileInvalid, AgencyProfileLocalityMax)
	}
	if len(p.DefaultRecipients) > AgencyProfileRecipientsMax {
		return fmt.Errorf("%w: at most %d default recipients", ErrAgencyProfileInvalid, AgencyProfileRecipientsMax)
	}
	for _, l := range p.DefaultRecipients {
		if utf8.RuneCountInString(l) > AgencyProfileTextMax {
			return fmt.Errorf("%w: a recipient line is longer than %d characters", ErrAgencyProfileInvalid, AgencyProfileTextMax)
		}
	}
	return nil
}
