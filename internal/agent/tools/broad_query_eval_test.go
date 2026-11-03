package tools

// Offline measurement of the broad-question gate on real questions against
// the real catalogue. Not part of the normal test run:
//
//	WEKNORA_BROADQ_EVAL=<items.jsonl> WEKNORA_BROADQ_DSN=<postgres dsn> \
//	  [WEKNORA_BROADQ_EVAL_MODELS=rules,qwen,gemma] [WEKNORA_BROADQ_EVAL_OUT=<out.jsonl>] \
//	  go test -run TestBroadQueryEval -count=1 -v ./internal/agent/tools/
//
// items.jsonl lines: {"id","text","label"}; label "card" (the card is the
// right answer), "answer" (it is not) or empty (unlabelled: only the rule
// result is written out). Every item runs in its own session, once with the
// rules alone and once per judge model (the production gate with Judge set).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/docformat"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
)

type broadEvalItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Label string `json:"label"`
}

func TestBroadQueryEval(t *testing.T) {
	path, dsn := os.Getenv("WEKNORA_BROADQ_EVAL"), os.Getenv("WEKNORA_BROADQ_DSN")
	if path == "" || dsn == "" {
		t.Skip("set WEKNORA_BROADQ_EVAL and WEKNORA_BROADQ_DSN")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	catalog := repository.NewLegalCatalogRepository(db)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var items []broadEvalItem
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var it broadEvalItem
		if err := json.Unmarshal(sc.Bytes(), &it); err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}
	f.Close()

	var out *os.File
	if p := os.Getenv("WEKNORA_BROADQ_EVAL_OUT"); p != "" {
		if out, err = os.Create(p); err != nil {
			t.Fatal(err)
		}
		defer out.Close()
	}
	ctx := context.Background()
	names := os.Getenv("WEKNORA_BROADQ_EVAL_MODELS")
	if names == "" {
		names = "rules"
	}
	rows := make([]map[string]any, len(items))
	for i, it := range items {
		rows[i] = map[string]any{"id": it.ID, "text": it.Text, "label": it.Label}
	}
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		var judge docformat.Completer
		if m, ok := intentEvalModelsByName()[name]; ok {
			model, err := chat.NewChat(&chat.ChatConfig{
				Source: types.ModelSourceRemote, ModelName: m.model, BaseURL: m.baseURL, APIKey: "none",
				Provider: string(provider.ProviderGeneric),
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			judge = docformat.ChatCompleter(model)
		} else if name != "rules" {
			t.Fatalf("unknown model %q", name)
		}
		fired, wrongCard, missedCard, labelled := 0, 0, 0, 0
		var lat []time.Duration
		for i, it := range items {
			start := time.Now()
			card := BroadQueryClarification(ctx, searcherFunc(catalog.SearchByTitle), fmt.Sprintf("eval-%s-%d", name, i),
				BroadQueryInput{Query: it.Text, Judge: judge})
			lat = append(lat, time.Since(start))
			topic := ""
			if card != nil {
				fired++
				topic = fmt.Sprintf("%s (%d)", card.Topic, card.Total)
			}
			rows[i][name] = topic
			if it.Label != "" {
				labelled++
				if card != nil && it.Label == "answer" {
					wrongCard++
					t.Logf("%s: card on %q → %s", name, it.Text, topic)
				}
				if card == nil && it.Label == "card" {
					missedCard++
					t.Logf("%s: no card on %q", name, it.Text)
				}
			}
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		t.Logf("== %s: %d items, card on %d; of %d labelled: card where none was wanted %d, no card where one was %d; p50 %v p95 %v",
			name, len(items), fired, labelled, wrongCard, missedCard,
			lat[len(lat)/2].Round(time.Millisecond), lat[len(lat)*95/100].Round(time.Millisecond))
	}
	if out != nil {
		for _, row := range rows {
			b, _ := json.Marshal(row)
			out.Write(append(b, '\n'))
		}
	}
}

func intentEvalModelsByName() map[string]struct{ baseURL, model string } {
	return map[string]struct{ baseURL, model string }{
		"qwen":  {"http://10.10.0.240:8000/v1", "Qwen/Qwen3.6-35B-A3B-FP8"},
		"gemma": {"http://10.10.0.240:8001/v1", "gemma-4-e4b-it"},
	}
}

type searcherFunc func(ctx context.Context, words []string, limit int) ([]*types.LegalDocument, error)

func (f searcherFunc) SearchTitles(ctx context.Context, words []string, limit int) ([]*types.LegalDocument, error) {
	return f(ctx, words, limit)
}
