package decision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// echoServer answers every noul question with 0.9 and records the request.
func echoServer(t *testing.T, wrap bool, gotPath, gotModel *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Errorf("Authorization = %q", got)
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		*gotModel = req.Model
		answers := map[string]any{}
		for id := range req.Questions {
			answers[id] = map[string]any{"type": "noul", "noul": 0.9}
		}
		body := map[string]any{"model": req.Model, "answers": answers}
		if wrap {
			body = map[string]any{"result": body, "success": true}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func noul(id string) map[string]Question {
	return map[string]Question{id: {Type: QuestionNoul, Instructions: "q"}}
}

func TestJevEndpointAndBareResponse(t *testing.T) {
	var path, model string
	srv := echoServer(t, false, &path, &model)
	defer srv.Close()
	d, err := NewDecider(&Config{Provider: "jev", BaseURL: srv.URL + "/v1/", APIKey: "key", ModelName: "jev-latest"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Decide(context.Background(), "state", noul("a"))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/systemone" || model != "jev-latest" || *got["a"].Noul != 0.9 {
		t.Fatalf("path=%q model=%q answers=%+v", path, model, got)
	}
}

func TestClefEndpointAndEnvelope(t *testing.T) {
	var path, model string
	srv := echoServer(t, true, &path, &model)
	defer srv.Close()
	d, err := NewDecider(&Config{
		Provider: "clef", BaseURL: srv.URL, APIKey: "key",
		ModelName: "@cf/cloudflare/clef-flash", AccountID: "acc1",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Decide(context.Background(), "state", noul("a"))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/accounts/acc1/ai/run/@cf/cloudflare/clef-flash" || model != "clef-flash" || *got["a"].Noul != 0.9 {
		t.Fatalf("path=%q model=%q answers=%+v", path, model, got)
	}
}

func TestNewDeciderRejectsBadConfig(t *testing.T) {
	for name, cfg := range map[string]*Config{
		"clef without account": {Provider: "clef", ModelName: "clef"},
		"unknown provider":     {Provider: "openai", ModelName: "gpt"},
		"no model":             {Provider: "jev"},
	} {
		if _, err := NewDecider(cfg); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestConfigFromModelReadsAccountID(t *testing.T) {
	cfg := ConfigFromModel(&types.Model{Name: "clef", Parameters: types.ModelParameters{
		Provider: "clef", APIKey: "k", ExtraConfig: map[string]string{"account_id": " acc "},
	}})
	if cfg.AccountID != "acc" || cfg.Provider != "clef" || cfg.ModelName != "clef" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestDecideErrors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"unsuccessful": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"result":null,"success":false,"errors":[{"message":"bad token"}]}`))
		},
		"status": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"missing": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"answers":{}}`))
		},
	}
	for name, h := range cases {
		srv := httptest.NewServer(h)
		d, _ := NewDecider(&Config{Provider: "jev", BaseURL: srv.URL, ModelName: "jev-latest"})
		_, err := d.Decide(context.Background(), "s", noul("a"))
		if err == nil {
			t.Errorf("%s: want error", name)
		} else if name == "unsuccessful" && !strings.Contains(err.Error(), "bad token") {
			t.Errorf("unsuccessful: err = %v", err)
		}
		srv.Close()
	}
	d, _ := NewDecider(&Config{Provider: "jev", BaseURL: "http://unused", ModelName: "jev-latest"})
	big := map[string]Question{}
	for i := 0; i <= MaxQuestions; i++ {
		big[string(rune('a'+i%26))+strings.Repeat("x", i)] = Question{Type: QuestionNoul}
	}
	if _, err := d.Decide(context.Background(), "s", big); err == nil {
		t.Error("over the question limit: want error")
	}
}
