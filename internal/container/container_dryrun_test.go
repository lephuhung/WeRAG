package container

import (
	"fmt"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// BuildContainer registers every provider, decorator and Invoke the server
// starts with; a duplicate decorator or an Invoke before the providers it
// needs panics at startup. In dry-run dig checks the graph without calling
// any constructor or invoked function, so nothing connects to a database,
// Redis or any other service. Run with and without REDIS_ADDR (it picks
// the task queue providers), then the Invokes cmd/server makes.
func TestBuildContainerDryRun(t *testing.T) {
	for _, redis := range []string{"", "127.0.0.1:1"} {
		t.Run(fmt.Sprintf("REDIS_ADDR=%q", redis), func(t *testing.T) {
			t.Setenv("REDIS_ADDR", redis)
			var c *dig.Container
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("BuildContainer panicked (the server would not start): %v", r)
					}
				}()
				c = BuildContainer(dig.New(dig.DryRun(true)))
			}()
			// cmd/server/bootstrap.go
			require.NoError(t, c.Invoke(func(interfaces.TenantAPIKeyService) {}))
			require.NoError(t, c.Invoke(func(interfaces.UserService, interfaces.TenantService, interfaces.TenantMemberService) {}))
			// cmd/server/main.go
			require.NoError(t, c.Invoke(func(*config.Config, *gin.Engine, interfaces.ResourceCleaner, interfaces.SystemSettingService) {}))
		})
	}
}
