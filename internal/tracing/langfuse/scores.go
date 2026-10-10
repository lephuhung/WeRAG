package langfuse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Score is a numeric judgement attached to a trace through the Langfuse
// public API (POST /api/public/scores). Scores are not part of the OTel
// stream: they usually arrive long after the trace was exported, e.g. when a
// user rates an answer.
type Score struct {
	// ID makes the call idempotent: posting the same ID again replaces the
	// earlier value, so a changed vote does not leave two scores behind.
	ID       string
	TraceID  string
	Name     string
	Value    float64
	Comment  string
	Metadata map[string]interface{}
}

const scoreRequestTimeout = 10 * time.Second

// UpsertScore creates or replaces a score. A disabled manager returns nil.
func (m *Manager) UpsertScore(ctx context.Context, s Score) error {
	if !m.Enabled() {
		return nil
	}
	if s.ID == "" || s.TraceID == "" || s.Name == "" {
		return fmt.Errorf("langfuse: score needs id, trace id and name")
	}
	body := map[string]interface{}{
		"id":       s.ID,
		"traceId":  s.TraceID,
		"name":     s.Name,
		"value":    s.Value,
		"dataType": "NUMERIC",
		// Always sent: an upsert that omits the field keeps the previous
		// comment, so a changed vote would carry a stale note.
		"comment": s.Comment,
	}
	if len(s.Metadata) > 0 {
		body["metadata"] = s.Metadata
	}
	if m.cfg.Environment != "" {
		body["environment"] = m.cfg.Environment
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("langfuse: encode score: %w", err)
	}
	return m.scoreRequest(ctx, http.MethodPost, "/api/public/scores", payload)
}

// DeleteScore removes a score by ID. A score that does not exist is not an
// error, so clearing a vote that never reached Langfuse is harmless.
func (m *Manager) DeleteScore(ctx context.Context, id string) error {
	if !m.Enabled() || id == "" {
		return nil
	}
	return m.scoreRequest(ctx, http.MethodDelete, "/api/public/scores/"+url.PathEscape(id), nil)
}

func (m *Manager) scoreRequest(ctx context.Context, method, path string, payload []byte) error {
	timeout := m.cfg.RequestTimeout
	if timeout <= 0 {
		timeout = scoreRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(m.cfg.Host, "/")+path, body)
	if err != nil {
		return fmt.Errorf("langfuse: build score request: %w", err)
	}
	creds := m.cfg.PublicKey + ":" + m.cfg.SecretKey
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(creds)))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("langfuse: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if method == http.MethodDelete && resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("langfuse: %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// TraceIDFromTraceparent returns the trace id of a W3C traceparent
// ("00-<trace-id>-<span-id>-<flags>"), or "" when it is not one.
func TraceIDFromTraceparent(traceparent string) string {
	parts := strings.Split(strings.TrimSpace(traceparent), "-")
	if len(parts) != 4 || len(parts[1]) != 32 || strings.Trim(parts[1], "0") == "" {
		return ""
	}
	for _, r := range parts[1] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return ""
		}
	}
	return parts[1]
}
