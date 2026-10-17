package middleware

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// MemoryOptOutHeader lets one request keep out of long-term memory entirely:
// nothing is recalled into the prompt and nothing the user says in that turn
// is distilled afterwards.
//
// It exists for traffic that is not a person talking about their own work —
// evaluation harnesses, smoke tests, scripted regressions — which otherwise
// runs under a real account and teaches memory that the account holder wants
// an iPhone, works at a commune People's Committee and keeps asking the
// assistant to e-mail the Director. Those were all test scenarios.
const MemoryOptOutHeader = "X-WeRAG-Memory"

// MemoryOptOut honours MemoryOptOutHeader. Only an explicit "off" disables
// memory; every other value, including absence, leaves the workspace and agent
// settings in charge.
func MemoryOptOut() gin.HandlerFunc {
	return func(c *gin.Context) {
		if memoryOptedOut(c.GetHeader(MemoryOptOutHeader)) {
			c.Request = c.Request.WithContext(types.WithMemoryDisabled(c.Request.Context()))
		}
		c.Next()
	}
}

func memoryOptedOut(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "0", "false", "none":
		return true
	}
	return false
}
