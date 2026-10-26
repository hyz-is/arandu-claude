# Working on Claude

This is an Arandu package that is a library: it puts the Anthropic Messages API
behind one call, `Complete`, and an interface, `Completer`, with a `Fake`. It
owns no table, no route and no policy, and it does not import the framework, so
an application on any framework version can install it. It is a Go module
somebody `go get`s and builds by hand in their own `bootstrap/app.go`.

Read `.agents/skills/` before writing code.

## The gates

Nothing is finished until all four exit zero.

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go')
go build ./...
go vet ./...
go test -race ./...
```

`GOWORK=off` is required: this checkout may sit beside a Go workspace that does
not list it.

## What this repository holds

| file | role |
| --- | --- |
| `claude.go` | `Config`, `New`, `Request`, `Result`, `Complete`, the errors |
| `fake.go` | `Fake`, the scripted `Completer` for an application's tests |
| `tests/Unit/claude_test.go` | the client against a local stand-in of the API, and the fake |

## Rules

- **Call Claude only through the official SDK** (`github.com/anthropics/anthropic-sdk-go`). Never raw HTTP, never an OpenAI-compatible shim.
- **Check the stop reason before the content.** A refusal, `max_tokens`, an empty answer or invalid JSON for a structured request is an error. A `Result` is always a whole answer.
- **The key comes from `Config`.** This package never reads the environment, never logs the key, and never puts a prompt or an answer in an error message.
- **Default model `claude-opus-5-5`**, with the server-side fallback on (`fallbacks: "default"` plus the `server-side-fallback-2026-07-01` beta). Do not append date suffixes to model ids.
- **No framework import.** If one is ever needed, the `framework` floor in `arandu.mod.toml` and in `go.mod` move together, and every installer is told in `UPGRADE.md`.
- **A test never calls the real API.** It uses `httptest` or the `Fake`.

## Commits

The identity is set in this checkout: `HYZIS <admin@hyz.is>`. No AI attribution
of any kind. Work goes to `develop`, and releases are tags on `main`.
