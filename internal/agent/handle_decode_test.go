package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testHandleDocID   = "02d47635-f6d5-4dc5-ab4d-769692a13d70"
	testHandleChunkID = "f0c3adcf-c5cd-4fef-923c-c9db94ff5da0"
)

// The non-streaming agent rounds (grounding gate, calculation gate, forced
// tool) used to hand the model's dN/cN handles straight to the tools. The
// tools take durable ids, so search_document_section failed with
// "knowledge_id d20 is not a document in the current scope" and read_document
// with "id c6 is neither a document (dN) nor a chunk (cN)". Every non-stream
// round must decode the handles the same way the streaming round does.
func TestNonStreamRoundsDecodeModelHandles(t *testing.T) {
	cases := []struct {
		name string
		// register returns the handle the model would see and the durable
		// value it must decode back to.
		register func(engine *AgentEngine) (handle, durable string)
		tool     string
		key      string
		ctx      func() context.Context
		run      func(engine *AgentEngine, ctx context.Context, opts *chat.ChatOptions) (*types.ChatResponse, error)
	}{
		{
			name: "grounding gate",
			register: func(engine *AgentEngine) (string, string) {
				return engine.modelContext.RegisterDocument(testHandleDocID), testHandleDocID
			},
			tool: "search_document_section",
			key:  "knowledge_id",
			ctx:  context.Background,
			run: func(engine *AgentEngine, ctx context.Context, opts *chat.ChatOptions) (*types.ChatResponse, error) {
				return engine.ungroundedRound(ctx, []chat.Message{{Role: "user", Content: "Điều 10?"}}, opts, 0, "sess")
			},
		},
		{
			name: "calculation gate",
			register: func(engine *AgentEngine) (string, string) {
				engine.modelContext.RegisterChunk(modelcontext.ChunkReference{
					ChunkID:     testHandleChunkID,
					KnowledgeID: testHandleDocID,
				})
				return engine.modelContext.ChunkHandle(testHandleChunkID), testHandleChunkID
			},
			tool: "read_document",
			key:  "id",
			ctx: func() context.Context {
				return context.WithValue(context.Background(), calcGateKey{}, []string{"calculate_household_tax"})
			},
			run: func(engine *AgentEngine, ctx context.Context, opts *chat.ChatOptions) (*types.ChatResponse, error) {
				return engine.calcGateRound(ctx, []chat.Message{{Role: "user", Content: "2 tỷ đóng bao nhiêu?"}}, opts, 0, "sess")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := &fixedResponseChat{}
			engine := newTestEngine(t, model)
			handle, durable := tc.register(engine)
			require.NotEmpty(t, handle)

			args, err := json.Marshal(map[string]string{
				tc.key:  handle,
				"query": "keep me",
			})
			require.NoError(t, err)
			model.resp = &types.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls: []types.LLMToolCall{{
					ID: "call-1",
					Function: types.FunctionCall{
						Name:      tc.tool,
						Arguments: string(args),
					},
				}},
			}

			resp, err := tc.run(engine, tc.ctx(), &chat.ChatOptions{})
			require.NoError(t, err)
			require.Len(t, resp.ToolCalls, 1)

			var got map[string]string
			require.NoError(t, json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &got))
			assert.Equal(t, durable, got[tc.key], "the model handle must be decoded before the tool runs")
			assert.Equal(t, "keep me", got["query"], "other arguments are left alone")
		})
	}
}

// fixedResponseChat returns one prepared response from Chat.
type fixedResponseChat struct {
	resp *types.ChatResponse
}

func (f *fixedResponseChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	if f.resp == nil {
		return nil, fmt.Errorf("no response prepared")
	}
	return f.resp, nil
}

func (f *fixedResponseChat) ChatStream(
	context.Context, []chat.Message, *chat.ChatOptions,
) (<-chan types.StreamResponse, error) {
	return nil, fmt.Errorf("not streamed")
}

func (f *fixedResponseChat) GetModelName() string { return "fixed-mock" }
func (f *fixedResponseChat) GetModelID() string   { return "fixed-mock" }
