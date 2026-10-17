package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/errors"
)

// ErrorHandler là middleware xử lý lỗi ứng dụng
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Xử lý yêu cầu
		c.Next()

		// Kiểm tra có lỗi không
		if len(c.Errors) > 0 {
			// Lấy lỗi cuối cùng
			err := c.Errors.Last().Err

			// Kiểm tra có phải lỗi ứng dụng không
			if appErr, ok := errors.IsAppError(err); ok {
				// Trả về lỗi ứng dụng
				c.JSON(appErr.HTTPCode, gin.H{
					"success": false,
					"error": gin.H{
						"code":    appErr.Code,
						"message": appErr.Message,
						"details": appErr.Details,
					},
				})
				return
			}

			// Xử lý các loại lỗi khác
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error": gin.H{
					"code":    errors.ErrInternalServer,
					"message": "Internal server error",
				},
			})
		}
	}
}
