package types

import (
	"context"
	"fmt"
	"strings"
)

// Province is one of the 34 provincial-level units in force since the 2025
// merger. ID is the vbpl.vn administrative-unit ID of the unit, under which
// the portal files the documents of the merged former provinces too;
// FormerNames are those provinces' names, still printed on documents
// issued before the merger.
type Province struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Short       string   `json:"short"`
	FormerNames []string `json:"former_names,omitempty"`
}

// Provinces lists the provincial units, derived on 2026-10-12 from the
// issuing agencies of the 115,766 local documents on vbpl.vn (current name
// from documents issued since July 2025, former names from older ones).
var Provinces = []Province{
	{ID: "b1d85884-83d2-4b0e-9aee-8306e0b5c338", Name: "Tỉnh An Giang", Short: "An Giang", FormerNames: []string{"Kiên Giang"}},
	{ID: "ecedb3e0-ddd2-4832-a483-d6ab150f1fd0", Name: "Tỉnh Bắc Ninh", Short: "Bắc Ninh", FormerNames: []string{"Bắc Giang"}},
	{ID: "7353d425-1ec8-4bc5-a870-76e4fc9c57f8", Name: "Tỉnh Cao Bằng", Short: "Cao Bằng"},
	{ID: "c3d647ab-b1c4-4018-a2b8-dfd8fffe1b8c", Name: "Tỉnh Cà Mau", Short: "Cà Mau", FormerNames: []string{"Bạc Liêu"}},
	{ID: "e370ab2e-7901-4c46-a32b-1e32e6497e9a", Name: "Thành phố Cần Thơ", Short: "Cần Thơ", FormerNames: []string{"Hậu Giang", "Sóc Trăng"}},
	{ID: "1b72912a-fd07-4723-bfc0-53224a0aec1f", Name: "Tỉnh Gia Lai", Short: "Gia Lai", FormerNames: []string{"Bình Định"}},
	{ID: "8756d1af-99d6-464b-80a9-c549a7f28e25", Name: "Thành phố Huế", Short: "Huế", FormerNames: []string{"Thừa Thiên Huế"}},
	{ID: "ab856170-012c-11f1-82da-77e9c10967ec", Name: "Thành phố Hà Nội", Short: "Hà Nội"},
	{ID: "9fdf54d4-ed21-487b-ae49-86de9f1f2377", Name: "Tỉnh Hà Tĩnh", Short: "Hà Tĩnh"},
	{ID: "d40546e4-ad94-4851-b54f-26908de082cf", Name: "Tỉnh Hưng Yên", Short: "Hưng Yên", FormerNames: []string{"Thái Bình"}},
	{ID: "2989a7cf-3e7d-4dc0-b519-cf3261a7e2db", Name: "Thành phố Hải Phòng", Short: "Hải Phòng", FormerNames: []string{"Hải Dương"}},
	{ID: "cbe871f9-87de-42ce-8ff3-41dbd5e7f8c3", Name: "Thành phố Hồ Chí Minh", Short: "Hồ Chí Minh", FormerNames: []string{"Bà Rịa - Vũng Tàu", "Bình Dương"}},
	{ID: "7e55389d-d995-4136-8ae9-bdd11c367085", Name: "Tỉnh Khánh Hòa", Short: "Khánh Hòa", FormerNames: []string{"Ninh Thuận"}},
	{ID: "6d1a53d2-c4ee-46c1-9a0e-3da4c137fb5a", Name: "Tỉnh Lai Châu", Short: "Lai Châu"},
	{ID: "21506483-bfc8-4d1d-a242-89697c8e588b", Name: "Tỉnh Lào Cai", Short: "Lào Cai", FormerNames: []string{"Yên Bái"}},
	{ID: "5b0158dc-b29e-4b36-8fb5-26bd45678694", Name: "Tỉnh Lâm Đồng", Short: "Lâm Đồng", FormerNames: []string{"Bình Thuận", "Đắk Nông"}},
	{ID: "8d85a89c-fae9-49c4-b0e2-e3dda4cbea71", Name: "Tỉnh Lạng Sơn", Short: "Lạng Sơn"},
	{ID: "143be0a1-1722-469c-84f6-fce261d10629", Name: "Tỉnh Nghệ An", Short: "Nghệ An"},
	{ID: "a22a6f1a-f64a-4bae-8e9e-40ea8f4673d0", Name: "Tỉnh Ninh Bình", Short: "Ninh Bình", FormerNames: []string{"Hà Nam", "Nam Định"}},
	{ID: "3e3a8809-b04b-4ef0-9fc8-2daf47460869", Name: "Tỉnh Phú Thọ", Short: "Phú Thọ", FormerNames: []string{"Hòa Bình", "Vĩnh Phú", "Vĩnh Phúc"}},
	{ID: "acb1009f-e3ad-4d28-b98e-486d2835573d", Name: "Tỉnh Quảng Ngãi", Short: "Quảng Ngãi", FormerNames: []string{"Kon Tum"}},
	{ID: "c63af856-a8d2-4961-8f8c-04b96307fb58", Name: "Tỉnh Quảng Ninh", Short: "Quảng Ninh"},
	{ID: "96d5dfc3-112a-49fc-829b-7a79d277154f", Name: "Tỉnh Quảng Trị", Short: "Quảng Trị", FormerNames: []string{"Quảng Bình"}},
	{ID: "6344dfb1-bacd-46e2-9235-d20f9cee852e", Name: "Tỉnh Sơn La", Short: "Sơn La"},
	{ID: "7979da5a-86fe-4992-9693-ae9eafdf157b", Name: "Tỉnh Thanh Hóa", Short: "Thanh Hóa"},
	{ID: "6d731f57-5ae8-4202-91f5-2f0d1760028c", Name: "Tỉnh Thái Nguyên", Short: "Thái Nguyên", FormerNames: []string{"Bắc Kạn"}},
	{ID: "7f5bf16e-53f7-47e2-9a13-94a1fa44a1f0", Name: "Tỉnh Tuyên Quang", Short: "Tuyên Quang", FormerNames: []string{"Hà Giang"}},
	{ID: "e367d549-4cc4-4da8-9807-e4cf3e272b22", Name: "Tỉnh Tây Ninh", Short: "Tây Ninh", FormerNames: []string{"Long An"}},
	{ID: "006f13cf-d662-4055-bc1a-07852f00346b", Name: "Tỉnh Vĩnh Long", Short: "Vĩnh Long", FormerNames: []string{"Bến Tre", "Trà Vinh"}},
	{ID: "189bbddf-1dfe-401d-9170-74b5cf14a180", Name: "Tỉnh Điện Biên", Short: "Điện Biên"},
	{ID: "0ce6fc0e-7ec1-421b-850d-9ec80d70119e", Name: "Thành phố Đà Nẵng", Short: "Đà Nẵng", FormerNames: []string{"Quảng Nam"}},
	{ID: "0a01a464-adf7-4344-a674-4b04787fa718", Name: "Tỉnh Đắk Lắk", Short: "Đắk Lắk", FormerNames: []string{"Phú Yên"}},
	{ID: "c14b5c2e-8215-4f8c-b48c-220cb9294d10", Name: "Tỉnh Đồng Nai", Short: "Đồng Nai", FormerNames: []string{"Bình Phước"}},
	{ID: "dd35eb78-fc91-4d1a-acdf-c8c48c9578ac", Name: "Tỉnh Đồng Tháp", Short: "Đồng Tháp", FormerNames: []string{"Tiền Giang"}},
}

// ProvinceByID returns the province with the given vbpl unit ID.
func ProvinceByID(id string) (Province, bool) {
	for _, p := range Provinces {
		if p.ID == id {
			return p, true
		}
	}
	return Province{}, false
}

// MaxWorkUnitLength bounds the free-text work unit of a user profile.
const MaxWorkUnitLength = 200

// UserContextPrompt renders the requesting user's profile (work unit and
// province) for the system prompt, or "" when the context carries no user
// or the profile is empty. The province is the default for local documents
// whose số hiệu every province reuses (NQ-HĐND, QĐ-UBND…).
func UserContextPrompt(ctx context.Context) string {
	user, _ := ctx.Value(UserContextKey).(*User)
	if user == nil {
		return ""
	}
	var lines []string
	if u := user.Preferences.WorkUnit; u != nil && strings.TrimSpace(*u) != "" {
		lines = append(lines, "- Work unit: "+strings.TrimSpace(*u))
	}
	if id := user.Preferences.ProvinceID; id != nil {
		if p, ok := ProvinceByID(*id); ok {
			line := "- Province/city: " + p.Name
			if len(p.FormerNames) > 0 {
				line += fmt.Sprintf(" (includes the former %s)", strings.Join(p.FormerNames, ", "))
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n## User context\nThe person asking stated this in their profile:\n" + strings.Join(lines, "\n") +
		"\nA local document number (NQ-HĐND, QĐ-UBND, …) can exist in every province: read it as this " +
		"province's unless the user names another, and say which province you assumed."
}
