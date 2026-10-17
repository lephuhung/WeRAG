package tools

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/redis/go-redis/v9"
)

// A template request runs over several turns (needs_input, the user
// answers, …) under one request ID: each turn adds the user's message to
// the request's state, so the draft is made from everything they said for
// it and nothing else of the conversation. With Redis
// (UseDocumentTemplateRedis) the state survives a restart.

const (
	templateStateTTL       = 24 * time.Hour
	templateStateKeyPrefix = "werag:doctemplate:"
	templateInputsMax      = 20
)

// DocumentTemplateState is what one template request has gathered.
type DocumentTemplateState struct {
	TemplateID string            `json:"template_id"`
	DocumentID string            `json:"document_id"`
	Inputs     []string          `json:"inputs"`
	Asked      map[string]string `json:"asked,omitempty"`
	// Redo: the request started from the last ready one (KeepBlanks).
	Redo      bool      `json:"redo,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type documentTemplateStore struct {
	mu      sync.Mutex
	entries map[string]*DocumentTemplateState
	rdb     *redis.Client
}

var documentTemplates = &documentTemplateStore{entries: map[string]*DocumentTemplateState{}}

// UseDocumentTemplateRedis keeps template request states in Redis; nil
// keeps them in process memory.
func UseDocumentTemplateRedis(rdb *redis.Client) {
	documentTemplates.rdb = rdb
}

func templateStateKey(sessionID, requestID string) string {
	return templateStateKeyPrefix + sessionID + ":" + requestID
}

// LastDocumentTemplateRequestID is the request ID the state of the session's
// last ready request of a template type is also kept under (a redo starts
// from it). Client request IDs are UUIDs and never take this form.
func LastDocumentTemplateRequestID(templateID string) string {
	return "last/" + templateID
}

// LoadDocumentTemplateState returns the state of a request of the session,
// nil when none is kept.
func LoadDocumentTemplateState(ctx context.Context, sessionID, requestID string) *DocumentTemplateState {
	key := templateStateKey(sessionID, requestID)
	s := documentTemplates
	if s.rdb == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		st := s.entries[key]
		if st == nil || time.Since(st.UpdatedAt) > templateStateTTL {
			delete(s.entries, key)
			return nil
		}
		cp := *st
		cp.Inputs = append([]string(nil), st.Inputs...)
		return &cp
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	raw, err := s.rdb.Get(rctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			logger.Warnf(ctx, "[DocumentTemplate] redis get %s: %v", key, err)
		}
		return nil
	}
	var st DocumentTemplateState
	if json.Unmarshal(raw, &st) != nil {
		return nil
	}
	return &st
}

// SaveDocumentTemplateState keeps the state of a request (the last
// templateInputsMax messages).
func SaveDocumentTemplateState(ctx context.Context, sessionID, requestID string, st *DocumentTemplateState) {
	if n := len(st.Inputs); n > templateInputsMax {
		st.Inputs = st.Inputs[n-templateInputsMax:]
	}
	st.UpdatedAt = time.Now()
	key := templateStateKey(sessionID, requestID)
	s := documentTemplates
	if s.rdb == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.entries[key] = st
		for k, e := range s.entries {
			if time.Since(e.UpdatedAt) > templateStateTTL {
				delete(s.entries, k)
			}
		}
		return
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.rdb.Set(rctx, key, raw, templateStateTTL).Err(); err != nil {
		logger.Warnf(ctx, "[DocumentTemplate] redis set %s: %v", key, err)
	}
}
