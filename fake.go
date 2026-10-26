package claude

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// Fake answers from a script, in memory, and records what it was asked. It
// is what an application's tests pass where production passes *Client.
//
//	fake := claude.NewFake(claude.Answer{JSON: `{"fit": 80}`})
//	svc := services.NewProposals(db, fake)
//	...
//	fake.Requests()[0].Prompt // what the service asked
type Fake struct {
	mu       sync.Mutex
	script   []Answer
	requests []Request
}

// Answer is one scripted answer: the text, or the JSON for a structured
// request, or an error.
type Answer struct {
	Text string
	JSON string
	Err  error
}

var _ Completer = (*Fake)(nil)

// ErrScriptExhausted is what a Fake answers once its script has run out.
var ErrScriptExhausted = errors.New("claude: the fake has no scripted answer left")

// NewFake builds a fake that gives the answers in order.
func NewFake(answers ...Answer) *Fake { return &Fake{script: answers} }

// Push appends answers to the script.
func (f *Fake) Push(answers ...Answer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = append(f.script, answers...)
}

// Complete records the request and gives the next scripted answer.
func (f *Fake) Complete(_ context.Context, req Request) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.script) == 0 {
		return Result{}, ErrScriptExhausted
	}
	a := f.script[0]
	f.script = f.script[1:]
	if a.Err != nil {
		return Result{}, a.Err
	}
	r := Result{Text: a.Text, Model: "fake"}
	if a.JSON != "" {
		if !json.Valid([]byte(a.JSON)) {
			return Result{}, errors.New("claude: the fake's scripted JSON is not valid")
		}
		r.Text, r.JSON = a.JSON, json.RawMessage(a.JSON)
	}
	if r.Text == "" {
		return Result{}, ErrEmpty
	}
	return r, nil
}

// Requests are the requests received, in order.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}
