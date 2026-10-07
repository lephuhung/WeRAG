package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestToolExecutionTimeout(t *testing.T) {
	assert.Equal(t, 10*time.Minute+5*time.Second, toolExecutionTimeout("shell_exec"))
	assert.Equal(t, 60*time.Second, toolExecutionTimeout("web_fetch"))
	for _, method := range []string{"request_help", "tab_borrow"} {
		assert.Equal(t, 5*time.Minute+15*time.Second,
			toolExecutionTimeout("local_browser", `{"method":"`+method+`"}`))
	}
	for _, args := range []string{`{"method":"click"}`, `{"method":"observe"}`, `{`} {
		assert.Equal(t, 60*time.Second, toolExecutionTimeout("local_browser", args))
	}
}

func TestCheckDocumentFormatGetsReasoningBudget(t *testing.T) {
	// the format check runs a thinking-mode evaluation (~1 min): the
	// default 60s tool budget would cut it off
	if got := toolExecutionTimeout("check_document_format"); got < 3*time.Minute {
		t.Fatalf("check_document_format timeout = %v", got)
	}
	if got := toolExecutionTimeout("web_fetch"); got != defaultToolExecTimeout {
		t.Fatalf("other tools keep the default, got %v", got)
	}
}
