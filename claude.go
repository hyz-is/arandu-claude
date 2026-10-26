// Package claude puts Claude behind one call an Arandu application can make
// and a test can replace: a prompt goes in, text or a schema-checked JSON
// answer comes out, with what it cost.
//
// It is a thin layer over the official Anthropic Go SDK. The SDK keeps what
// it already does well -- the HTTP client, retries on 408, 409, 429 and 5xx
// (529 included), typed errors -- and this package adds the three things an
// application otherwise writes by hand every time: a configuration that is
// refused at boot rather than on the first request, the answer read only
// after the stop reason says it is whole, and an interface so the code that
// calls Claude is tested without the network.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultModel is the model asked when Config leaves Model empty.
const DefaultModel = "claude-opus-5-5"

// DefaultMaxTokens bounds one answer when neither Config nor Request says
// otherwise. It is generous on purpose: an answer cut at the cap is an error
// here (ErrTruncated), and a retry costs more than the headroom.
const DefaultMaxTokens = 16000

// Effort is how much the model should put into an answer: "low", "medium",
// "high", "xhigh" or "max".
type Effort string

// The effort levels the API accepts.
const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

func (e Effort) valid() bool {
	switch e {
	case "", EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	}
	return false
}

// Config is what the application passes when it builds the client. The key
// comes from the application's own environment; this package never reads
// the environment itself.
type Config struct {
	// APIKey is the Anthropic API key. Required.
	APIKey string
	// Model is the model id; empty means DefaultModel.
	Model string
	// Effort is the default effort; empty means EffortHigh. A Request can
	// ask for another.
	Effort Effort
	// MaxTokens is the default cap on one answer; zero means DefaultMaxTokens.
	MaxTokens int64
	// Timeout bounds one request, retries included; zero means two minutes.
	Timeout time.Duration
	// MaxRetries is how many times the SDK retries a retryable failure; a
	// negative value means none, zero means the SDK's default of two.
	MaxRetries int
	// BaseURL replaces the API's address, for a proxy or a test server.
	BaseURL string
	// HTTPClient replaces the SDK's HTTP client.
	HTTPClient *http.Client
	// NoFallback turns off the server-side fallback that re-serves a request
	// the model declined for policy reasons on the model's default
	// substitute. It is on by default.
	NoFallback bool
}

// Request is one question.
type Request struct {
	// System is the instruction that frames every answer: who is answering,
	// the rules, the template. It is sent as a cached system block, so a
	// long and stable System costs less from the second request on.
	System string
	// Prompt is the question itself. Required.
	Prompt string
	// Schema, when set, is the JSON schema the answer must satisfy; the
	// answer then arrives in Result.JSON. Objects in it need
	// "additionalProperties": false.
	Schema map[string]any
	// Effort and MaxTokens override the client's defaults for this request.
	Effort    Effort
	MaxTokens int64
}

// Usage is what a request cost, in tokens.
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// Result is a whole answer.
type Result struct {
	// Text is the answer's text. For a structured request it is the JSON.
	Text string
	// JSON is the answer for a request with a Schema; empty otherwise.
	JSON json.RawMessage
	// Model is the model that answered, which can differ from the one asked
	// when the server-side fallback served the request.
	Model string
	// ID is the message id, for correlating with the Anthropic console.
	ID    string
	Usage Usage
}

// Decode reads a structured answer into v.
func (r Result) Decode(v any) error {
	if len(r.JSON) == 0 {
		return errors.New("claude: the answer carries no JSON; the request had no Schema")
	}
	return json.Unmarshal(r.JSON, v)
}

// Completer is what calls Claude. *Client implements it against the API and
// *Fake in memory, so the code that depends on it is tested without either a
// key or the network.
type Completer interface {
	Complete(ctx context.Context, req Request) (Result, error)
}

// The ways an answer can fail after the API accepted the request.
var (
	// ErrRefused: the model declined the request for policy reasons, and no
	// fallback served it. Refusal carries the category.
	ErrRefused = errors.New("claude: the model declined the request")
	// ErrTruncated: the answer reached MaxTokens and stopped mid-way.
	ErrTruncated = errors.New("claude: the answer reached the token cap")
	// ErrEmpty: the answer carried no text.
	ErrEmpty = errors.New("claude: the answer is empty")
)

// Refusal is ErrRefused with what the API said about it.
type Refusal struct {
	Category    string
	Explanation string
}

func (r *Refusal) Error() string {
	if r.Category == "" {
		return ErrRefused.Error()
	}
	return fmt.Sprintf("%s (%s)", ErrRefused.Error(), r.Category)
}

// Unwrap makes errors.Is(err, ErrRefused) hold.
func (r *Refusal) Unwrap() error { return ErrRefused }

// Client calls the Anthropic API.
type Client struct {
	api       anthropic.Client
	model     string
	effort    Effort
	maxTokens int64
	fallback  bool
}

var _ Completer = (*Client)(nil)

// New builds the client, refusing a configuration that cannot work.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("claude: Config.APIKey is required")
	}
	if !cfg.Effort.valid() {
		return nil, fmt.Errorf("claude: Config.Effort %q is not one of low, medium, high, xhigh, max", cfg.Effort)
	}
	if cfg.MaxTokens < 0 {
		return nil, errors.New("claude: Config.MaxTokens cannot be negative")
	}
	c := &Client{model: strings.TrimSpace(cfg.Model), effort: cfg.Effort, maxTokens: cfg.MaxTokens, fallback: !cfg.NoFallback}
	if c.model == "" {
		c.model = DefaultModel
	}
	if c.effort == "" {
		c.effort = EffortHigh
	}
	if c.maxTokens == 0 {
		c.maxTokens = DefaultMaxTokens
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey), option.WithRequestTimeout(timeout)}
	switch {
	case cfg.MaxRetries < 0:
		opts = append(opts, option.WithMaxRetries(0))
	case cfg.MaxRetries > 0:
		opts = append(opts, option.WithMaxRetries(cfg.MaxRetries))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	c.api = anthropic.NewClient(opts...)
	return c, nil
}

// Model is the model the client asks by default.
func (c *Client) Model() string { return c.model }

// Complete asks one question and answers only a whole answer: a refusal, a
// cut answer or an empty one is an error, never a partial Result.
func (c *Client) Complete(ctx context.Context, req Request) (Result, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{}, errors.New("claude: Request.Prompt is required")
	}
	if !req.Effort.valid() {
		return Result{}, fmt.Errorf("claude: Request.Effort %q is not one of low, medium, high, xhigh, max", req.Effort)
	}
	effort := req.Effort
	if effort == "" {
		effort = c.effort
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.maxTokens
	}
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: maxTokens,
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(req.Prompt))},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffort(effort),
		},
	}
	if req.System != "" {
		params.System = []anthropic.BetaTextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}}
	}
	if req.Schema != nil {
		params.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{Schema: req.Schema}
	}
	if c.fallback {
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: "default"}
	}
	msg, err := c.api.Beta.Messages.New(ctx, params)
	if err != nil {
		return Result{}, fmt.Errorf("claude: %w", err)
	}
	return read(msg, req.Schema != nil)
}

// read turns a message into a Result, checking the stop reason before the
// content.
func read(msg *anthropic.BetaMessage, structured bool) (Result, error) {
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return Result{}, &Refusal{Category: string(msg.StopDetails.Category), Explanation: msg.StopDetails.Explanation}
	case anthropic.BetaStopReasonMaxTokens:
		return Result{}, ErrTruncated
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if b, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	r := Result{
		Text:  strings.TrimSpace(text.String()),
		Model: string(msg.Model),
		ID:    msg.ID,
		Usage: Usage{
			InputTokens:      msg.Usage.InputTokens,
			OutputTokens:     msg.Usage.OutputTokens,
			CacheReadTokens:  msg.Usage.CacheReadInputTokens,
			CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
		},
	}
	if r.Text == "" {
		return Result{}, ErrEmpty
	}
	if structured {
		if !json.Valid([]byte(r.Text)) {
			return Result{}, fmt.Errorf("claude: the structured answer is not valid JSON")
		}
		r.JSON = json.RawMessage(r.Text)
	}
	return r, nil
}
