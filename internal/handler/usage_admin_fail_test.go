package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
)

// A database error (the seats CHECK violation on dev) reached the buyer as
// the 500's details. Internal errors are logged, and the client only gets a
// generic message without details; AppErrors pass through as written.
func TestUsageAdminFailHidesInternalErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/billing/orders", nil)
		return c
	}

	sqlErr := errors.New(`ERROR: new row for relation "usage_orders" violates check constraint "chk_usage_orders_kind" (SQLSTATE 23514)`)
	c := newCtx()
	usageAdminFail(c, fmt.Errorf("create order: %w", sqlErr))
	require.Len(t, c.Errors, 1)
	var appErr *apperrors.AppError
	require.True(t, errors.As(c.Errors[0].Err, &appErr))
	require.Equal(t, apperrors.ErrInternalServer, appErr.Code)
	require.Nil(t, appErr.Details)
	require.NotContains(t, appErr.Message, "usage_orders")
	require.False(t, strings.Contains(appErr.Error(), "SQLSTATE"))

	c = newCtx()
	known := apperrors.NewBadRequestError("Gia hạn phải mua ít nhất 3 chỗ").WithDetails("seats_below_current")
	usageAdminFail(c, fmt.Errorf("wrapped: %w", known))
	require.Len(t, c.Errors, 1)
	require.Same(t, known, c.Errors[0].Err)
}
