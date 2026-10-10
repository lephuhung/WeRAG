package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type stubFeedbackService struct {
	interfaces.MessageService
	calls   int
	gotVote int
	gotNote string
	err     error
}

func (s *stubFeedbackService) SetMessageFeedback(
	_ context.Context, sessionID, messageID string, feedback int, comment string,
) (*types.Message, error) {
	s.calls++
	s.gotVote, s.gotNote = feedback, comment
	if s.err != nil {
		return nil, s.err
	}
	at := time.Date(2026, 10, 10, 7, 0, 0, 0, time.UTC)
	return &types.Message{ID: messageID, SessionID: sessionID, Feedback: feedback, FeedbackComment: comment, FeedbackAt: &at}, nil
}

func putFeedback(t *testing.T, svc interfaces.MessageService, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	h := &MessageHandler{MessageService: svc}
	r.PUT("/messages/:session_id/:id/feedback", h.SetMessageFeedback)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/messages/s1/m1/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestSetMessageFeedbackRecordsVote(t *testing.T) {
	svc := &stubFeedbackService{}
	w := putFeedback(t, svc, `{"value":-1,"comment":"  trích sai điều luật  "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if svc.gotVote != -1 || svc.gotNote != "trích sai điều luật" {
		t.Fatalf("service got vote=%d note=%q", svc.gotVote, svc.gotNote)
	}
	var resp struct {
		Data struct {
			Feedback        int    `json:"feedback"`
			FeedbackComment string `json:"feedback_comment"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Feedback != -1 || resp.Data.FeedbackComment != "trích sai điều luật" {
		t.Fatalf("response = %s", w.Body.String())
	}
}

func TestSetMessageFeedbackAcceptsClear(t *testing.T) {
	svc := &stubFeedbackService{}
	if w := putFeedback(t, svc, `{"value":0}`); w.Code != http.StatusOK || svc.calls != 1 || svc.gotVote != 0 {
		t.Fatalf("status = %d calls=%d vote=%d", w.Code, svc.calls, svc.gotVote)
	}
}

func TestSetMessageFeedbackRejectsBadInput(t *testing.T) {
	for name, body := range map[string]string{
		"missing value": `{"comment":"x"}`,
		"out of range":  `{"value":5}`,
		"long comment":  `{"value":1,"comment":"` + strings.Repeat("á", types.MessageFeedbackCommentMaxRunes+1) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			svc := &stubFeedbackService{}
			if w := putFeedback(t, svc, body); w.Code != http.StatusBadRequest || svc.calls != 0 {
				t.Fatalf("status = %d calls=%d", w.Code, svc.calls)
			}
		})
	}
}

func TestSetMessageFeedbackMapsServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{apperrors.ErrSessionNotFound, http.StatusNotFound},
		{apperrors.ErrFeedbackNotAllowed, http.StatusBadRequest},
	} {
		if w := putFeedback(t, &stubFeedbackService{err: tc.err}, `{"value":1}`); w.Code != tc.want {
			t.Fatalf("%v: status = %d, want %d", tc.err, w.Code, tc.want)
		}
	}
}
