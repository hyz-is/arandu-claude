# Changelog

Everything worth knowing about a release of Claude is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [Unreleased]

## [0.4.0] - 2026-11-26

### Added

- `Request.Documents` sends PDFs with the question: `Document{MediaType, Data}` (raw bytes), as base64 document blocks before the images and the prompt, in the same user message. The model reads the text and the pages of the PDF, so a scanned PDF is read too. An empty `MediaType` is detected from the bytes.
- A document that cannot be sent (other type, empty, bytes that are not a PDF, over `MaxDocumentBytes` (32 MB), more than `MaxDocuments` (5)) is refused before the call with an error wrapping `ErrBadDocument`. `Document.Validate()` runs the same check.
- `Fake` records the documents in `Requests()`.

## [0.3.0] - 2026-11-15

### Added

- `Request.Images` sends pictures with the question: `Image{MediaType, Data}` (raw bytes), as base64 image blocks before the prompt, in the same user message. Accepts `image/jpeg`, `image/png`, `image/webp` and `image/gif`; an empty `MediaType` is detected from the bytes.
- An image that cannot be sent (other type, empty, over `MaxImageBytes` (5 MB), a declared type the bytes contradict, more than `MaxImages` (100)) is refused before the call with an error wrapping `ErrBadImage`. `Image.Validate()` runs the same check.
- `Fake` records the images in `Requests()`.

## [0.2.0] - 2026-10-26

### Added

- `Config.WorkspaceID` names the workspace a user API key (one not scoped to a workspace) runs in, sent as the `anthropic-workspace-id` header. Without it the API refuses such a key with 400.

## [0.1.0] - 2026-10-26

### Added

- `New(Config)` builds a `*Client` over the official Anthropic Go SDK (v1.84.0), refusing a missing key, an unknown effort or a negative token cap.
- `Complete(ctx, Request)` asks one question and returns only a whole answer. Text, or JSON checked against `Request.Schema`. A refusal (`ErrRefused`, with `*Refusal` carrying the category), an answer cut at the cap (`ErrTruncated`), an empty answer (`ErrEmpty`) and invalid JSON are errors.
- Defaults: model `claude-opus-5-5`, effort `high`, 16000 tokens, two-minute timeout, the SDK's retries, and the server-side fallback for policy refusals (`NoFallback` turns it off).
- `System` is sent as a cached block. `Result` carries the model that answered, the message id and the token usage.
- `Completer` and `Fake`, a scripted `Completer` that records the requests, for an application's tests.

