package container

import "testing"

// resolveDBPoolLimits is what decides how many PostgreSQL connections one
// process may hold, so the override parsing and the maxIdle clamp are worth
// pinning: a silently ignored override would leave operators believing they
// had raised a limit they had not.
func TestResolveDBPoolLimits(t *testing.T) {
	tests := []struct {
		name       string
		open, idle string
		wantOpen   int
		wantIdle   int
	}{
		{
			name:     "unset keeps defaults",
			wantOpen: defaultDBMaxOpenConns,
			wantIdle: defaultDBMaxIdleConns,
		},
		{
			name: "both overridden", open: "80", idle: "30",
			wantOpen: 80, wantIdle: 30,
		},
		{
			name: "surrounding spaces are trimmed", open: " 12 ", idle: " 6 ",
			wantOpen: 12, wantIdle: 6,
		},
		{
			name: "unparseable falls back", open: "abc", idle: "x",
			wantOpen: defaultDBMaxOpenConns, wantIdle: defaultDBMaxIdleConns,
		},
		{
			name: "zero falls back", open: "0", idle: "0",
			wantOpen: defaultDBMaxOpenConns, wantIdle: defaultDBMaxIdleConns,
		},
		{
			name: "negative falls back", open: "-5", idle: "-1",
			wantOpen: defaultDBMaxOpenConns, wantIdle: defaultDBMaxIdleConns,
		},
		{
			// database/sql would quietly reduce MaxIdleConns to MaxOpenConns;
			// clamping here keeps the logged value honest.
			name: "idle above open is clamped to open", open: "10", idle: "50",
			wantOpen: 10, wantIdle: 10,
		},
		{
			name: "idle clamped against a defaulted open", idle: "999",
			wantOpen: defaultDBMaxOpenConns, wantIdle: defaultDBMaxOpenConns,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set both variables unconditionally, empty included: a case that
			// relied on the variable being absent would turn red on a machine
			// (or CI image) that happens to export it. An empty value is
			// treated as unset by resolveDBPoolLimits, so this is equivalent
			// without depending on the ambient environment.
			t.Setenv("DB_MAX_OPEN_CONNS", tt.open)
			t.Setenv("DB_MAX_IDLE_CONNS", tt.idle)
			gotOpen, gotIdle := resolveDBPoolLimits()
			if gotOpen != tt.wantOpen || gotIdle != tt.wantIdle {
				t.Fatalf("resolveDBPoolLimits() = (%d, %d), want (%d, %d)",
					gotOpen, gotIdle, tt.wantOpen, tt.wantIdle)
			}
			if gotIdle > gotOpen {
				t.Fatalf("maxIdle %d must never exceed maxOpen %d", gotIdle, gotOpen)
			}
		})
	}
}
