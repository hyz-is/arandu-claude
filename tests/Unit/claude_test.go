package unit_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hyz-is/arandu-claude"
)

// api is a stand-in for the Messages API: it records each request and
// answers what reply returns for the n-th call.
type api struct {
	calls    atomic.Int32
	bodies   []map[string]any
	headers  []http.Header
	reply    func(n int) (int, string)
	captured chan struct{}
}

func (a *api) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(a.calls.Add(1))
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		a.bodies = append(a.bodies, body)
		a.headers = append(a.headers, r.Header.Clone())
		status, out := a.reply(n)
		w.Header().Set("Content-Type", "application/json")
		if status == http.StatusTooManyRequests {
			w.Header().Set("retry-after-ms", "1")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, out)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func message(stop, text string) string {
	m := map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
		"stop_reason": stop,
		"content":     []map[string]any{{"type": "text", "text": text}},
		"usage":       map[string]any{"input_tokens": 120, "output_tokens": 45, "cache_read_input_tokens": 100, "cache_creation_input_tokens": 0},
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func client(t *testing.T, srv *httptest.Server, cfg claude.Config) *claude.Client {
	t.Helper()
	cfg.APIKey = "test-key"
	cfg.BaseURL = srv.URL
	c, err := claude.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var schema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"fit": map[string]any{"type": "integer"}},
	"required":             []string{"fit"},
	"additionalProperties": false,
}

// TestAStructuredAnswerArrivesDecodedWithWhatItCost: the request carries the
// default model and effort, the schema, the cached system block and the
// server-side fallback; the answer is decoded and its usage reported.
func TestAStructuredAnswerArrivesDecodedWithWhatItCost(t *testing.T) {
	a := &api{reply: func(int) (int, string) { return 200, message("end_turn", `{"fit": 82}`) }}
	c := client(t, a.server(t), claude.Config{})
	res, err := c.Complete(context.Background(), claude.Request{System: "Você avalia projetos.", Prompt: "Projeto X", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Fit int }
	if err := res.Decode(&out); err != nil || out.Fit != 82 {
		t.Fatalf("decoded %+v (%v)", out, err)
	}
	if res.Usage.InputTokens != 120 || res.Usage.OutputTokens != 45 || res.Usage.CacheReadTokens != 100 || res.Model != "claude-opus-5-5" {
		t.Fatalf("result metadata %+v", res)
	}
	body := a.bodies[0]
	if body["model"] != claude.DefaultModel {
		t.Errorf("model %v", body["model"])
	}
	oc, _ := body["output_config"].(map[string]any)
	if oc["effort"] != "high" || oc["format"] == nil {
		t.Errorf("output_config %v", oc)
	}
	if body["fallbacks"] != "default" || !strings.Contains(a.headers[0].Get("anthropic-beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("fallback not requested: %v / %q", body["fallbacks"], a.headers[0].Get("anthropic-beta"))
	}
	sys, _ := body["system"].([]any)
	if len(sys) != 1 || sys[0].(map[string]any)["cache_control"] == nil {
		t.Errorf("system block not cached: %v", body["system"])
	}
	if a.headers[0].Get("X-Api-Key") != "test-key" {
		t.Error("the key was not sent")
	}
}

// TestARateLimitIsRetried: a 429 is retried by the SDK and the second answer
// is the one returned.
func TestARateLimitIsRetried(t *testing.T) {
	a := &api{reply: func(n int) (int, string) {
		if n == 1 {
			return http.StatusTooManyRequests, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`
		}
		return 200, message("end_turn", "ok")
	}}
	c := client(t, a.server(t), claude.Config{})
	res, err := c.Complete(context.Background(), claude.Request{Prompt: "oi"})
	if err != nil || res.Text != "ok" || a.calls.Load() != 2 {
		t.Fatalf("text %q, calls %d, err %v", res.Text, a.calls.Load(), err)
	}
}

// TestAnAnswerThatIsNotWholeIsAnError: a refusal, a cut answer, an empty
// answer and invalid JSON never come back as a Result.
func TestAnAnswerThatIsNotWholeIsAnError(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		schema map[string]any
		want   error
	}{
		{"refusal", `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"no"},"content":[],"usage":{"input_tokens":1,"output_tokens":0}}`, nil, claude.ErrRefused},
		{"truncated", message("max_tokens", "metade"), nil, claude.ErrTruncated},
		{"empty", message("end_turn", "  "), nil, claude.ErrEmpty},
		{"invalid json", message("end_turn", "não é json"), schema, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &api{reply: func(int) (int, string) { return 200, tc.body }}
			c := client(t, a.server(t), claude.Config{})
			_, err := c.Complete(context.Background(), claude.Request{Prompt: "p", Schema: tc.schema})
			if err == nil {
				t.Fatal("an incomplete answer came back as a result")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
		})
	}
	var refusal *claude.Refusal
	a := &api{reply: func(int) (int, string) { return 200, cases[0].body }}
	_, err := client(t, a.server(t), claude.Config{}).Complete(context.Background(), claude.Request{Prompt: "p"})
	if !errors.As(err, &refusal) || refusal.Category != "cyber" {
		t.Fatalf("the refusal lost its category: %v", err)
	}
}

// TestTheRequestOverridesTheDefaultsAndTheFallbackCanBeTurnedOff.
func TestTheRequestOverridesTheDefaultsAndTheFallbackCanBeTurnedOff(t *testing.T) {
	a := &api{reply: func(int) (int, string) { return 200, message("end_turn", "ok") }}
	c := client(t, a.server(t), claude.Config{Model: "claude-sonnet-5-5", NoFallback: true, MaxTokens: 2000})
	if _, err := c.Complete(context.Background(), claude.Request{Prompt: "p", Effort: claude.EffortLow, MaxTokens: 500}); err != nil {
		t.Fatal(err)
	}
	body := a.bodies[0]
	oc, _ := body["output_config"].(map[string]any)
	if body["model"] != "claude-sonnet-5-5" || oc["effort"] != "low" || body["max_tokens"] != float64(500) {
		t.Errorf("overrides not applied: %v", body)
	}
	if _, ok := body["fallbacks"]; ok {
		t.Error("the fallback was sent although it was turned off")
	}
}

// TestTheConfigurationRefusesWhatCannotWork.
func TestTheConfigurationRefusesWhatCannotWork(t *testing.T) {
	for name, cfg := range map[string]claude.Config{
		"no key":       {},
		"bad effort":   {APIKey: "k", Effort: "turbo"},
		"negative cap": {APIKey: "k", MaxTokens: -1},
	} {
		if _, err := claude.New(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c, err := claude.New(claude.Config{APIKey: "k"})
	if err != nil || c.Model() != claude.DefaultModel {
		t.Fatalf("defaults: %v %v", c, err)
	}
	if _, err := c.Complete(context.Background(), claude.Request{}); err == nil {
		t.Error("an empty prompt was sent")
	}
}

// TestTheFakeAnswersInOrderAndRecordsTheRequests.
func TestTheFakeAnswersInOrderAndRecordsTheRequests(t *testing.T) {
	var _ claude.Completer = (*claude.Fake)(nil)
	f := claude.NewFake(claude.Answer{JSON: `{"fit": 40}`}, claude.Answer{Err: claude.ErrTruncated})
	ctx := context.Background()
	res, err := f.Complete(ctx, claude.Request{Prompt: "um", Schema: schema})
	var out struct{ Fit int }
	if err != nil || res.Decode(&out) != nil || out.Fit != 40 {
		t.Fatalf("first answer: %+v %v", res, err)
	}
	if _, err := f.Complete(ctx, claude.Request{Prompt: "dois"}); !errors.Is(err, claude.ErrTruncated) {
		t.Fatalf("second answer: %v", err)
	}
	if _, err := f.Complete(ctx, claude.Request{Prompt: "três"}); !errors.Is(err, claude.ErrScriptExhausted) {
		t.Fatalf("exhausted: %v", err)
	}
	if got := f.Requests(); len(got) != 3 || got[1].Prompt != "dois" {
		t.Fatalf("requests %+v", got)
	}
}
