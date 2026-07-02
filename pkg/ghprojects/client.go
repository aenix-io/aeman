package ghprojects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultEndpoint is the GitHub GraphQL API endpoint.
const defaultEndpoint = "https://api.github.com/graphql"

// Sentinel errors returned by the client; callers can map them onto HTTP
// statuses with errors.Is.
var (
	// ErrBoardNotFound is returned when a project cannot be located.
	ErrBoardNotFound = errors.New("project board not found")
	// ErrCardNotFound is returned when an item id is not on the board.
	ErrCardNotFound = errors.New("card not found")
	// ErrFieldNotFound is returned when a board lacks a required field.
	ErrFieldNotFound = errors.New("field not found")
	// ErrNoContent is returned for operations needing an underlying issue/draft.
	ErrNoContent = errors.New("card has no underlying issue")
)

// Doer is the subset of *http.Client used by the client, so tests can inject a
// stub transport.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client talks to the GitHub Projects v2 GraphQL API with a fixed bearer token.
// It is cheap to construct, so callers build one per request with the token
// resolved from the active auth mode.
type Client struct {
	token      string
	endpoint   string
	httpClient Doer
	userAgent  string
	userCache  userIDCache
	// fieldCache memoises fields the domain setters lazily create, keyed by
	// project id + role, so two setters in one board operation never create the
	// same field twice (see ensureDomainField).
	fieldCache domainFieldCache
}

// Option customises a Client.
type Option func(*Client)

// WithEndpoint overrides the GraphQL endpoint (used in tests).
func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.endpoint = endpoint }
}

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(d Doer) Option {
	return func(c *Client) { c.httpClient = d }
}

// New builds a Client authenticating with the given token.
func New(token string, opts ...Option) *Client {
	c := &Client{
		token:      token,
		endpoint:   defaultEndpoint,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		userAgent:  "aeman",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// graphQLError wraps one or more errors returned in a GraphQL response. A
// GraphQL response can carry both partial data and errors at once (e.g. a
// NOT_FOUND on a nested field), so callers may still inspect the decoded data.
type graphQLError struct {
	messages []string
	types    []string
}

func (e *graphQLError) Error() string {
	return "github graphql: " + strings.Join(e.messages, "; ")
}

// isNotFound reports whether every error is a NOT_FOUND, i.e. the request
// failed only because a requested resource does not exist.
func (e *graphQLError) isNotFound() bool {
	if len(e.types) == 0 {
		return false
	}
	for _, t := range e.types {
		if t != "NOT_FOUND" {
			return false
		}
	}
	return true
}

// isNotFoundErr reports whether err is a GraphQL error made up solely of
// NOT_FOUND entries.
func isNotFoundErr(err error) bool {
	var ge *graphQLError
	return errors.As(err, &ge) && ge.isNotFound()
}

// graphql executes a GraphQL document and decodes the data field into out.
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("encode graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build graphql request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call github graphql: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read github graphql response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("github graphql: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode github graphql response (HTTP %d): %w", resp.StatusCode, err)
	}

	// Decode whatever data resolved before reporting errors: GitHub returns
	// partial data alongside errors (e.g. organization present but projectV2
	// null on a missing project), and callers rely on it to tell a missing
	// resource from a hard failure.
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil && len(envelope.Errors) == 0 {
			return fmt.Errorf("decode github graphql data: %w", err)
		}
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, len(envelope.Errors))
		types := make([]string, len(envelope.Errors))
		for i, e := range envelope.Errors {
			msgs[i] = e.Message
			types[i] = e.Type
		}
		return &graphQLError{messages: msgs, types: types}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github graphql: HTTP %d", resp.StatusCode)
	}
	return nil
}
