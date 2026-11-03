package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeLegalGraphFocus struct{ kbID, doc, q string }

func (f *fakeLegalGraphFocus) KBFocus(_ context.Context, kbID, doc, q string) (*types.LegalGraphFocus, error) {
	f.kbID, f.doc, f.q = kbID, doc, q
	switch doc {
	case "far":
		return nil, types.ErrLegalGraphFocusNotFound
	case "boom":
		return nil, errors.New("db down")
	}
	return &types.LegalGraphFocus{Focus: &types.LegalGraphDoc{ID: "law", DocumentNumber: "31/2024/QH15"}}, nil
}

func TestGetLegalGraphFocus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeLegalGraphFocus{}
	h := NewKnowledgeGraphViewHandler(nil, nil, nil, fake)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/knowledge-bases/:id/legal-graph/focus", h.GetLegalGraphFocus)
	get := func(query string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/knowledge-bases/kb-1/legal-graph/focus"+query, nil))
		return rec
	}

	rec := get("?doc=law&q=dat%20dai")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, fakeLegalGraphFocus{"kb-1", "law", "dat dai"}, *fake)
	var body struct {
		Success bool                  `json:"success"`
		Data    types.LegalGraphFocus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.Equal(t, "31/2024/QH15", body.Data.Focus.DocumentNumber)

	require.Equal(t, http.StatusNotFound, get("?doc=far").Code)
	require.Equal(t, http.StatusInternalServerError, get("?doc=boom").Code)
}
