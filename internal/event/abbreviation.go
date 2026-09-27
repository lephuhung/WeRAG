package event

import "github.com/Tencent/WeKnora/internal/types"

const EventAbbreviationResolution EventType = "abbreviation_resolution"

type AbbreviationResolutionData struct {
	types.AbbreviationPublicState
	Origin string `json:"origin"`
}
