<p align="center">
  <img src=".github/logo.svg" alt="Arandu" width="180">
</p>

# Claude for Arandu

Claude behind one call an Arandu application can make and a test can replace: a
prompt goes in, text or a schema-checked JSON answer comes out, with what it
cost.

It is a thin layer over the official
[Anthropic Go SDK](https://github.com/anthropics/anthropic-sdk-go). The SDK keeps
the HTTP client, the retries on 408, 409, 429 and 5xx, and the typed errors.
This package adds three things an application otherwise writes every time:

- a configuration refused at boot, not on the first request;
- an answer returned only when it is whole. A refusal, an answer cut at the
  token cap, an empty answer or invalid JSON is an error, never a partial
  result;
- the `Completer` interface, with a `Fake`, so the code that calls Claude is
  tested without a key or the network.

It owns no table, no route and no policy, and it does not import the
framework: an application on any framework version installs it.

## Install

```bash
go get github.com/hyz-is/arandu-claude@latest
```

## Wire it

The key comes from the application's environment, through its own config;
this package never reads the environment.

```go
// bootstrap/app.go
ai, err := claude.New(claude.Config{
    APIKey: cfg.Services.Anthropic.Secret, // ANTHROPIC_API_KEY
    // Model:  "claude-opus-5-5",          // the default
    // Effort: claude.EffortHigh,          // the default
})
if err != nil {
    return App{}, err
}
proposals := services.NewProposals(db, ai) // takes a claude.Completer
```

## Use it

```go
res, err := ai.Complete(ctx, claude.Request{
    System: proposalTemplate, // stable: sent as a cached block
    Prompt: project.Description,
    Schema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "fit":   map[string]any{"type": "integer"},
            "offer": map[string]any{"type": "integer"},
            "text":  map[string]any{"type": "string"},
        },
        "required":             []string{"fit", "offer", "text"},
        "additionalProperties": false,
    },
})
switch {
case errors.Is(err, claude.ErrRefused):   // declined for policy reasons
case errors.Is(err, claude.ErrTruncated): // reached the token cap
case err != nil:                          // network, API or invalid answer
}
var draft struct{ Fit, Offer int; Text string }
_ = res.Decode(&draft)
log.Info("claude", "model", res.Model, "in", res.Usage.InputTokens, "out", res.Usage.OutputTokens)
```

### With a picture

`Images` goes before the `Prompt` in the same message, as base64 image blocks.
`MediaType` may be left empty: it is detected from the bytes. A photo received
on WhatsApp, for instance:

```go
res, err := ai.Complete(ctx, claude.Request{
    System: agronomistRules,
    Prompt: "O que está acontecendo com esta folha de café?",
    Images: []claude.Image{{MediaType: "image/jpeg", Data: photoBytes}},
    Schema: diagnosisSchema, // optional; without it the answer is text in res.Text
})
if errors.Is(err, claude.ErrBadImage) {
    // not jpeg/png/webp/gif, empty, over 5 MB, or the bytes are not what was declared
}
```

Accepted: `image/jpeg`, `image/png`, `image/webp`, `image/gif`, up to
`claude.MaxImageBytes` (5 MB) each and `claude.MaxImages` (100) per request. A
bad image fails before the call, with an error that wraps `claude.ErrBadImage`;
`Image.Validate()` runs the same check on its own. The prompt is still required.

### With a PDF

`Documents` goes first in the same message, before the `Images` and the `Prompt`,
as base64 document blocks. The model reads the text of the PDF and looks at its
pages, so a scanned PDF works too:

```go
res, err := ai.Complete(ctx, claude.Request{
    System:    "Transcreva o plano alimentar.",
    Prompt:    "Leia o documento.",
    Documents: []claude.Document{{Data: pdfBytes}}, // MediaType may be empty
    Schema:    planSchema,
})
if errors.Is(err, claude.ErrBadDocument) {
    // not a PDF by its bytes, empty, over 32 MB, or more than 5 in one request
}
```

Up to `claude.MaxDocumentBytes` (32 MB) each and `claude.MaxDocuments` (5) per
request; the API reads at most 100 pages of PDF per request. `Document.Validate()`
runs the check on its own.

## Test with it

```go
fake := claude.NewFake(claude.Answer{JSON: `{"fit": 80, "offer": 6000, "text": "…"}`})
svc := services.NewProposals(db, fake)
// … exercise svc …
fake.Requests()[0].Prompt // what the service asked
fake.Requests()[0].Images // the pictures that came with it
fake.Requests()[0].Documents // the PDFs that came with it
```

## Config

| field | default | |
| --- | --- | --- |
| `APIKey` | required | the Anthropic API key |
| `Model` | `claude-opus-5-5` | any model id |
| `Effort` | `high` | `low`, `medium`, `high`, `xhigh`, `max`; a Request can override it |
| `MaxTokens` | 16000 | the cap on one answer; a Request can override it |
| `Timeout` | 2 minutes | one request, retries included |
| `MaxRetries` | the SDK's 2 | negative turns retries off |
| `BaseURL`, `HTTPClient` | the SDK's | for a proxy or a test server |
| `WorkspaceID` | none | the workspace a user key (not scoped to one) runs in |
| `NoFallback` | false | turns off the server-side fallback for policy refusals |

The server-side fallback (`fallbacks: "default"`) is on by default: when the
model declines a request for policy reasons, the API re-serves it on that
model's default substitute, and `Result.Model` says which model answered.

## License

MIT. See [LICENSE.md](LICENSE.md).
