package container

import (
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/utils"
)

func TestMain(m *testing.M) {
	// Wiring tests intentionally use loopback httptest servers.
	utils.SetSSRFWhitelistFromRaw("127.0.0.1,::1,localhost")
	code := m.Run()
	utils.SetSSRFWhitelistFromRaw("")
	os.Exit(code)
}
