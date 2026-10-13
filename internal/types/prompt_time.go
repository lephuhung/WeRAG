package types

import (
	"os"
	"time"
	_ "time/tzdata" // the zone must load even without system zoneinfo
)

// promptTimeZone is the zone of the dates shown to models ("current_time",
// "Current date"). The deployment serves Vietnam while the container clock
// zone (TZ) came from upstream as Asia/Shanghai, an hour ahead; prompts
// must not jump to tomorrow at 23:00 Vietnam time. PROMPT_TIMEZONE
// overrides it.
var promptTimeZone = loadPromptTimeZone()

func loadPromptTimeZone() *time.Location {
	name := os.Getenv("PROMPT_TIMEZONE")
	if name == "" {
		name = "Asia/Ho_Chi_Minh"
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.Local
}

// PromptNow is the current time in the zone dates are shown to models in.
func PromptNow() time.Time {
	return time.Now().In(promptTimeZone)
}
