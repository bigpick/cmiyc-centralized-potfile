// Package client is the CLI side. It talks to the pool over HTTP only, so the
// operator's machine (which holds PGP keys and the real contest email account)
// is the sole place secrets live. The pool never sees key material.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zilla/cmiyc-pool/internal/store"
)

// Pool is a typed client for the server API.
type Pool struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewPool builds a client with a sane default timeout. Uploads of large
// potfiles get a longer per-request timeout in Upload.
func NewPool(baseURL, token string) *Pool {
	return &Pool{
		BaseURL: trimSlash(baseURL),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

type linesResponse struct {
	Lines []store.CrackLine `json:"lines"`
	Count int               `json:"count"`
}

// Upload streams a potfile to POST /v1/submit and returns the dedupe summary.
func (p *Pool) Upload(ctx context.Context, author string, body io.Reader) (store.SubmitResult, error) {
	var res store.SubmitResult
	u := fmt.Sprintf("%s/v1/submit?author=%s", p.BaseURL, urlQueryEscape(author))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return res, err
	}
	req.Header.Set("Content-Type", "text/plain")
	p.authorize(req)

	// Uploads can be large; give this request its own generous timeout.
	cli := &http.Client{Timeout: 10 * time.Minute}
	resp, err := cli.Do(req)
	if err != nil {
		return res, err
	}
	defer resp.Body.Close()
	if err := expectOK(resp); err != nil {
		return res, err
	}
	return res, json.NewDecoder(resp.Body).Decode(&res)
}

// Pending fetches cracks not yet sent to KoreLogic.
func (p *Pool) Pending(ctx context.Context) ([]store.CrackLine, error) {
	return p.getLines(ctx, "/v1/pending")
}

// All fetches every crack, each tagged with was_pending.
func (p *Pool) All(ctx context.Context) ([]store.CrackLine, error) {
	return p.getLines(ctx, "/v1/all")
}

func (p *Pool) getLines(ctx context.Context, path string) ([]store.CrackLine, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	p.authorize(req)
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := expectOK(resp); err != nil {
		return nil, err
	}
	var lr linesResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, err
	}
	return lr.Lines, nil
}

// Mark reports a confirmed KoreLogic send back to the pool, transitioning the
// given ids to submitted. Returns how many actually transitioned.
func (p *Pool) Mark(ctx context.Context, ids []int64) (int64, error) {
	payload, err := json.Marshal(map[string][]int64{"ids": ids})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/v1/mark", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	p.authorize(req)
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if err := expectOK(resp); err != nil {
		return 0, err
	}
	var out struct {
		Marked int64 `json:"marked"`
	}
	return out.Marked, json.NewDecoder(resp.Body).Decode(&out)
}

// Stats fetches the internal analytics snapshot.
func (p *Pool) Stats(ctx context.Context) (store.Stats, error) {
	var st store.Stats
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/v1/stats", nil)
	if err != nil {
		return st, err
	}
	p.authorize(req)
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if err := expectOK(resp); err != nil {
		return st, err
	}
	return st, json.NewDecoder(resp.Body).Decode(&st)
}

func (p *Pool) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.Token)
}

func expectOK(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	return fmt.Errorf("pool returned %s: %s", resp.Status, bytes.TrimSpace(b))
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func urlQueryEscape(s string) string {
	// small local escape to avoid importing net/url for one field
	var b bytes.Buffer
	for _, r := range []byte(s) {
		switch {
		case r == ' ':
			b.WriteString("%20")
		case r == '&':
			b.WriteString("%26")
		case r == '=':
			b.WriteString("%3D")
		case r == '?':
			b.WriteString("%3F")
		case r == '#':
			b.WriteString("%23")
		case r == '%':
			b.WriteString("%25")
		default:
			b.WriteByte(r)
		}
	}
	return b.String()
}
