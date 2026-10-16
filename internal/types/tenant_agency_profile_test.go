package types

import (
	"errors"
	"strings"
	"testing"
)

func TestTenantAgencyProfileValidate(t *testing.T) {
	p := TenantAgencyProfile{AgencyName: "  Sở   Nội vụ ", AgencyAbbr: " SNV ", Locality: "Huế",
		DefaultRecipients: StringArray{" - UBND tỉnh; ", "", "  "}}
	p.Normalize()
	if err := p.Validate(); err != nil || p.AgencyName != "Sở Nội vụ" || len(p.DefaultRecipients) != 1 {
		t.Fatalf("normalized %+v: %v", p, err)
	}
	for name, bad := range map[string]TenantAgencyProfile{
		"no name":       {},
		"abbr space":    {AgencyName: "X", AgencyAbbr: "S NV"},
		"abbr slash":    {AgencyName: "X", AgencyAbbr: "SNV/VP"},
		"locality date": {AgencyName: "X", Locality: "Huế, ngày"},
		"long name":     {AgencyName: strings.Repeat("a", AgencyProfileTextMax+1)},
	} {
		bad.Normalize()
		if err := bad.Validate(); !errors.Is(err, ErrAgencyProfileInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
