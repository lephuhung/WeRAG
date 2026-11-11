package container

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/dig"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// dig takes one decorator per type and scope: a second Decorate of a type
// panics in BuildContainer at startup. Every decorator of the container is
// registered here on a fresh container, as BuildContainer does.
func TestContainerDecoratorsOnePerType(t *testing.T) {
	c := dig.New()
	for _, list := range allDecorators {
		for _, d := range list {
			require.NoError(t, c.Decorate(d))
		}
	}
	// the guard works: another decorator of a decorated type is refused
	require.ErrorContains(t, c.Decorate(service.NewBillingEmailNotifications), "already decorated")
	require.ErrorContains(t, c.Decorate(service.NewLegalRequestAddedNotifications), "already decorated")
}

// Every Decorate of container.go goes through decorate (and so through
// allDecorators, which the test above registers).
func TestContainerDecoratesOnlyThroughLists(t *testing.T) {
	src, err := os.ReadFile("container.go")
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(src), ".Decorate("),
		"a Decorate outside decorate(): add the decorator to a list in allDecorators instead")
	require.Equal(t, len(allDecorators), strings.Count(string(src), "decorate(container, "),
		"every list of allDecorators is registered by BuildContainer")
}

type stubBilling struct{ interfaces.UsageBillingService }
type stubLegalRequests struct{ interfaces.LegalRequestService }

// The composed decorators resolve with the real constructors and wrap the
// provided services.
func TestContainerNotificationDecoratorsResolve(t *testing.T) {
	c := dig.New()
	inner := &stubBilling{}
	requests := &stubLegalRequests{}
	require.NoError(t, c.Provide(func() interfaces.UsageBillingService { return inner }))
	require.NoError(t, c.Provide(func() interfaces.LegalRequestService { return requests }))
	require.NoError(t, c.Provide(func() *service.EmailNotifier { return nil }))
	require.NoError(t, c.Provide(func() *service.LegalRequestNotifier { return nil }))
	require.NoError(t, c.Provide(func() *service.SuperAdminQueueNotifier { return nil }))
	decorate(c, notificationDecorators)
	require.NoError(t, c.Invoke(func(b interfaces.UsageBillingService, r interfaces.LegalRequestService) {
		require.NotSame(t, inner, b, "billing decorated")
		require.NotSame(t, requests, r, "legal requests decorated")
	}))
}
