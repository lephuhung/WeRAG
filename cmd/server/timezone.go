package main

import (
	"time"
	_ "time/tzdata" // the zone must load even without system zoneinfo
)

// The deployment serves Vietnam: logs, generated file names, dates read back
// from the database and "today" are all Vietnam time (GMT+7), whatever zone
// the container was started with (upstream compose defaulted TZ to
// Asia/Shanghai). Vietnam has no DST, so the fixed offset is exact.
func init() {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		loc = time.FixedZone("ICT", 7*3600)
	}
	time.Local = loc
}
