package router

import (
	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/handler"
)

// RegisterMemoryRoutes registers the personal long-term memory endpoints.
//
// There is no subject parameter anywhere in the paths: the memory space is
// derived from the caller's principal, so these routes can only ever read or
// write the caller's own memories. Memory is configured platform-wide by the
// SuperAdmin and users do not manage it themselves, so the whole group is
// SystemAdmin-only; members still build memory through the conversation.
func RegisterMemoryRoutes(r *gin.RouterGroup, memoryHandler *handler.MemoryHandler, g *rbacGuards) {
	if memoryHandler == nil {
		return
	}
	memoryGroup := g.apiKeyGroup(r.Group("/memory", g.SystemAdmin()), apiKeyFullAccess())
	{
		memoryGroup.GET("/settings", memoryHandler.GetSettings)
		memoryGroup.PUT("/settings", memoryHandler.UpdateSettings)
		memoryGroup.GET("/items", memoryHandler.ListItems)
		memoryGroup.POST("/items", memoryHandler.CreateItem)
		memoryGroup.DELETE("/items", memoryHandler.Clear)
		memoryGroup.PUT("/items/:id", memoryHandler.UpdateItem)
		memoryGroup.DELETE("/items/:id", memoryHandler.DeleteItem)
		memoryGroup.POST("/items/:id/confirm", memoryHandler.ConfirmItem)
		memoryGroup.GET("/topics", memoryHandler.ListTopics)
		memoryGroup.DELETE("/topics/:id", memoryHandler.DeleteTopic)
		memoryGroup.POST("/topics/:id/promote", memoryHandler.PromoteTopic)
		memoryGroup.GET("/documents", memoryHandler.ListDocuments)
		memoryGroup.DELETE("/documents/:id", memoryHandler.DeleteDocument)
		memoryGroup.GET("/export", memoryHandler.Export)
		memoryGroup.POST("/consolidate", memoryHandler.Consolidate)
	}

	// The one thing every user may do with their memory: take back a memory
	// the assistant just announced it saved ("I just saved ... — not right?").
	// The scope comes from the caller, so only the caller's own item can go.
	correction := g.apiKeyGroup(r.Group("/memory", g.Member()), apiKeyFullAccess())
	correction.POST("/items/:id/reject", memoryHandler.RejectItem)
}
