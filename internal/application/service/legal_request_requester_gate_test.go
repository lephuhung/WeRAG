package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

// The gate keeps who asked (user, organization, conversation, message,
// channel) next to the request; a turn without a user account keeps nobody.
func TestLegalMissingDocumentGateRecordsRequester(t *testing.T) {
	nd53 := &types.LegalDocument{ID: "155366", DocumentNumber: "53/2022/NĐ-CP", DocType: "Nghị định",
		Title: "Quy định chi tiết một số điều của Luật An ninh mạng", Status: "Còn hiệu lực"}
	catalog := &gateCatalog{reports: map[string][]*types.LegalStatusReport{"53/2022/ND-CP": {{Document: nd53}}}}
	runIn := func(ctx context.Context, session *types.Session, channel, messageID string) *gateRequests {
		t.Helper()
		reqs := &gateRequests{}
		s := &sessionService{legalCatalog: catalog}
		s.SetLegalRequests(reqs, &gateReviews{approved: map[string]bool{}})
		req := &types.QARequest{Query: "Nghị định 53/2022/NĐ-CP quy định gì?", Session: session, Channel: channel,
			UserMessageID: messageID, CustomAgent: &types.CustomAgent{ID: types.BuiltinSmartReasoningID}}
		if _, handled, err := s.legalMissingDocumentGate(ctx, req, event.NewEventBus(), []string{"kb-own"}); err != nil || !handled {
			t.Fatalf("gate: handled=%v err=%v", handled, err)
		}
		return reqs
	}
	run := func(session *types.Session, channel, messageID string) *gateRequests {
		t.Helper()
		return runIn(context.Background(), session, channel, messageID)
	}

	reqs := run(&types.Session{ID: "s1", TenantID: 10003, UserID: "u1"}, "word_addin", "msg-1")
	if len(reqs.requesters) != 1 {
		t.Fatalf("requesters = %+v", reqs.requesters)
	}
	got := reqs.requesters[0]
	if got.RequestID != "r155366" || got.UserID != "u1" || got.TenantID != 10003 || got.SessionID != "s1" ||
		got.Channel != types.LegalRequesterChannelWordAddin || got.MessageID == nil || *got.MessageID != "msg-1" {
		t.Fatalf("requester = %+v", got)
	}

	if reqs := run(&types.Session{ID: "s2", TenantID: 10003, UserID: "u2"}, "", ""); len(reqs.requesters) != 1 ||
		reqs.requesters[0].MessageID != nil || reqs.requesters[0].Channel != "" {
		t.Fatalf("no message id / channel: %+v", reqs.requesters)
	}
	// an API caller is not a person to tell
	if reqs := run(&types.Session{ID: "s3", TenantID: 10003, UserID: types.SessionOwnerAPITenantKeyPrefix + "7"}, "api", ""); len(reqs.requesters) != 0 {
		t.Fatalf("API principal recorded: %+v", reqs.requesters)
	}
	if reqs := run(&types.Session{ID: "s4", TenantID: 10003}, "web", ""); len(reqs.requesters) != 0 {
		t.Fatalf("no user recorded: %+v", reqs.requesters)
	}
	// a Guest in the public portal
	portal := context.WithValue(context.Background(), types.TenantInfoContextKey,
		&types.Tenant{ID: 10012, Kind: types.TenantKindPublicPortal})
	if reqs := runIn(portal, &types.Session{ID: "s5", TenantID: 10012, UserID: "guest"}, "web", ""); len(reqs.requesters) != 1 ||
		reqs.requesters[0].Channel != types.LegalRequesterChannelPortal {
		t.Fatalf("portal requester: %+v", reqs.requesters)
	}
}
