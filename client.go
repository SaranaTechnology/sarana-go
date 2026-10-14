// Package sarana is the Go client for the Sarana payment API (/api/v2).
//
//	c := sarana.New("sk_live_...")
//	inv, err := c.CreateInvoice(ctx, sarana.InvoiceRequest{Amount: 150000, ExternalID: "ORDER-1"})
//
// Every call returns the "data" field of the API envelope. List calls also
// return the pagination meta.
package sarana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ProductionURL is the default base URL.
	ProductionURL = "https://api.internal.saranatechnology.com/api/v2"
	// SandboxURL points at the staging API, which talks to the Xendit sandbox.
	SandboxURL = "https://api.internal-go.saranatechnology.com/api/v2"

	// Version is sent in the User-Agent header.
	Version = "0.1.0"
)

// Object is a decoded JSON object from the API.
type Object = map[string]any

// Meta is the pagination block of list responses.
type Meta struct {
	Total       int `json:"total"`
	PerPage     int `json:"per_page"`
	CurrentPage int `json:"current_page"`
	LastPage    int `json:"last_page"`
}

// List is one page of a list endpoint.
type List struct {
	Data []Object `json:"data"`
	Meta *Meta    `json:"meta,omitempty"`
}

// Error is returned for every non-2xx response.
type Error struct {
	Status  int
	Message string
	Body    []byte
}

func (e *Error) Error() string {
	return fmt.Sprintf("sarana: HTTP %d: %s", e.Status, e.Message)
}

// Client calls the API with one API key. It is safe for concurrent use.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	userAgent  string
	subHeader  string // "for-user-id" or "X-Sub-Client"
	subValue   string
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the base URL, e.g. SandboxURL.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient replaces the default http.Client (30s timeout).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithUserAgent appends an identifier to the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = c.userAgent + " " + ua }
}

// New returns a client for the given API key.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		baseURL:    ProductionURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  "sarana-go/" + Version,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ForSubClient returns a copy of the client that acts on behalf of a
// sub-client, identified by its sub_id (sent as the for-user-id header).
func (c *Client) ForSubClient(subID string) *Client {
	cp := *c
	cp.subHeader, cp.subValue = "for-user-id", subID
	return &cp
}

// ForSubRef is ForSubClient by sub_ref (sent as the X-Sub-Client header).
func (c *Client) ForSubRef(subRef string) *Client {
	cp := *c
	cp.subHeader, cp.subValue = "X-Sub-Client", subRef
	return &cp
}

type envelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Meta    *Meta           `json:"meta"`
}

// do sends the request and decodes the envelope. scoped=false leaves out the
// sub-client header for endpoints where the API rejects or ignores it.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, scoped bool) (*envelope, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if scoped && c.subValue != "" {
		req.Header.Set(c.subHeader, c.subValue)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var env envelope
	jsonErr := json.Unmarshal(raw, &env)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := env.Message
		if jsonErr != nil || msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, &Error{Status: resp.StatusCode, Message: msg, Body: raw}
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("sarana: decode response: %w", jsonErr)
	}
	return &env, nil
}

func (c *Client) object(ctx context.Context, method, path string, query url.Values, body any, scoped bool) (Object, error) {
	env, err := c.do(ctx, method, path, query, body, scoped)
	if err != nil {
		return nil, err
	}
	var out Object
	if len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, &out); err != nil {
			return nil, fmt.Errorf("sarana: decode data: %w", err)
		}
	}
	return out, nil
}

func (c *Client) list(ctx context.Context, path string, query url.Values, scoped bool) (*List, error) {
	env, err := c.do(ctx, http.MethodGet, path, query, nil, scoped)
	if err != nil {
		return nil, err
	}
	out := &List{Data: []Object{}, Meta: env.Meta}
	if len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, &out.Data); err != nil {
			return nil, fmt.Errorf("sarana: decode data: %w", err)
		}
	}
	return out, nil
}

// raw decodes data into any shape (arrays without meta, objects).
func (c *Client) raw(ctx context.Context, path string, scoped bool) (any, error) {
	env, err := c.do(ctx, http.MethodGet, path, nil, nil, scoped)
	if err != nil {
		return nil, err
	}
	return decodeAny(env.Data)
}

func decodeAny(data json.RawMessage) (any, error) {
	var out any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("sarana: decode data: %w", err)
		}
	}
	return out, nil
}

func seg(s string) string { return url.PathEscape(s) }
