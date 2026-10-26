---
name: claude-package
description: Install, wire and use the Claude package (github.com/hyz-is/arandu-claude) in an Arandu application. Use when the request is to "call Claude", "use the Claude API", "add claude to the app", "go get github.com/hyz-is/arandu-claude", "wire claude into bootstrap/app.go", "test code that calls Claude", "structured output", "the answer came back cut", "ErrRefused", "ErrTruncated", or when a project's go.mod already requires github.com/hyz-is/arandu-claude. Covers the key in the application's config, the one line of wiring, Complete with a Schema, the errors, the Fake for tests, and why no prompt or answer goes into a log.
license: MIT
metadata:
  audience: app
---

# Using Claude

A library: `claude.New(Config)` builds a `*claude.Client`, which implements
`claude.Completer`. It has no table, no route and no policy, so there is no
`k.Register`, no migration and no permission to open.

## Install

```bash
go get github.com/hyz-is/arandu-claude@<latest tag>   # gh release list -R hyz-is/arandu-claude
```

## Wire it, in bootstrap/app.go

1. Put the key in the application's config (`config/services.go`), read from
   `ANTHROPIC_API_KEY`, and add the empty variable to `.env.example`.
2. Build the client once, only when the key is configured:

   ```go
   var ai claude.Completer
   if cfg.Services.Anthropic.Configured() {
       c, err := claude.New(claude.Config{APIKey: cfg.Services.Anthropic.Secret})
       if err != nil {
           return App{}, err
       }
       ai = c
   }
   ```

3. Pass it to the services that need it, typed as `claude.Completer`. A service
   with a nil Completer answers "not configured" instead of calling.

## Use it

- `Complete(ctx, claude.Request{System, Prompt, Schema})`. Keep `System` stable
  (template, rules); it is sent as a cached block. Put what varies in `Prompt`.
- With a `Schema` (objects need `"additionalProperties": false`), read the answer
  with `res.Decode(&v)`.
- Errors: `claude.ErrRefused` (with `*claude.Refusal` carrying the category),
  `claude.ErrTruncated`, `claude.ErrEmpty`, and SDK errors for the network and
  the API. Never treat an error as a partial answer.
- Call it from a job, not from a request handler: an answer can take a minute.
- Log `res.Model` and `res.Usage`, never the prompt or the answer. They can carry
  personal data.

## Test with it

```go
fake := claude.NewFake(claude.Answer{JSON: `{"fit": 80}`}, claude.Answer{Err: claude.ErrRefused})
// pass fake where production passes the client
fake.Requests() // what was asked, in order
```
