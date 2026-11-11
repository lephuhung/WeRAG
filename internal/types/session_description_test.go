package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeClientSessionDescriptionReservedMarkers(t *testing.T) {
	im := IMSessionDescription("telegram", false)
	embed := EmbedSessionMarkerPrefix + "ch-1"
	skill := SkillMaintenanceSessionMarker + "install"
	for _, marker := range []string{im, IMSessionDescription("slack", true), embed, skill} {
		require.Equal(t, "", SanitizeClientSessionDescription(marker, ""), "create drops %q", marker)
		require.Equal(t, "", SanitizeClientSessionDescription(marker, "ghi chú"), "update drops %q", marker)
		require.Equal(t, marker, SanitizeClientSessionDescription("", marker), "an update without description keeps %q", marker)
		require.Equal(t, marker, SanitizeClientSessionDescription("khác", marker), "an update cannot replace %q", marker)
	}
	require.Equal(t, "ghi chú", SanitizeClientSessionDescription("ghi chú", "cũ"))
}
