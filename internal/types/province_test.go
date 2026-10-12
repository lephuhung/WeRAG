package types

import (
	"context"
	"strings"
	"testing"
)

func TestProvinces(t *testing.T) {
	if len(Provinces) != 34 {
		t.Fatalf("want the 34 post-merger units, got %d", len(Provinces))
	}
	hue, ok := ProvinceByID("8756d1af-99d6-464b-80a9-c549a7f28e25")
	if !ok || hue.Name != "Thành phố Huế" || len(hue.FormerNames) != 1 || hue.FormerNames[0] != "Thừa Thiên Huế" {
		t.Fatalf("Huế: %+v %v", hue, ok)
	}
}

func TestUserContextPrompt(t *testing.T) {
	if UserContextPrompt(context.Background()) != "" {
		t.Fatal("no user, no context")
	}
	unit, hue := "Công an thành phố Huế", "8756d1af-99d6-464b-80a9-c549a7f28e25"
	user := &User{Preferences: UserPreferences{WorkUnit: &unit, ProvinceID: &hue}}
	got := UserContextPrompt(context.WithValue(context.Background(), UserContextKey, user))
	for _, want := range []string{"- Work unit: Công an thành phố Huế",
		"- Province/city: Thành phố Huế (includes the former Thừa Thiên Huế)", "NQ-HĐND"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if UserContextPrompt(context.WithValue(context.Background(), UserContextKey, &User{})) != "" {
		t.Error("an empty profile adds nothing")
	}
}
