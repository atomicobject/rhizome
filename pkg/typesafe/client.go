package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.typesafe.ai"
	DefaultModel   = "jev-latest"
)

// Client is reusable across goroutines. It never reads environment variables,
// modifies caller-owned requests, logs credentials, or performs vault work.
type Client struct {
	apiKey           string
	baseURL          string
	model            string
	http             *http.Client
	timeout          time.Duration
	maxRetries       int
	maxResponseBytes int64
}

// Option configures a Client. Invalid options fail in NewClient.
type Option func(*Client)

// WithBaseURL changes the API root (without /v1). HTTP is supported for local
// tests; use HTTPS for remote services. Redirects are never followed.
func WithBaseURL(baseURL string) Option { return func(c *Client) { c.baseURL = baseURL } }

// WithModel changes the default model; a Request.Model overrides it per batch.
func WithModel(model string) Option { return func(c *Client) { c.model = model } }

// WithHTTPClient injects the transport, connection pool, and HTTP timeout. The
// client is copied and its redirect policy replaced, leaving the original intact.
func WithHTTPClient(client *http.Client) Option { return func(c *Client) { c.http = client } }

// WithTimeout sets the total budget per call, including all retries and waits.
// The default is 30 seconds. A shorter context deadline always wins.
func WithTimeout(timeout time.Duration) Option { return func(c *Client) { c.timeout = timeout } }

// WithMaxRetries sets retries after the first attempt (default two). Zero
// disables retries. Only HTTP 408, 429, and 5xx responses are retried; ambiguous
// transport failures are returned to the caller without replaying paid requests.
func WithMaxRetries(retries int) Option { return func(c *Client) { c.maxRetries = retries } }

// WithMaxResponseBytes sets the body limit per response (default 16 MiB).
func WithMaxResponseBytes(limit int64) Option {
	return func(c *Client) { c.maxResponseBytes = limit }
}

// NewClient validates configuration and constructs a client. API keys are
// supplied explicitly; the zero Client value is not usable.
func NewClient(apiKey string, options ...Option) (*Client, error) {
	c := &Client{
		apiKey: strings.TrimSpace(apiKey), baseURL: DefaultBaseURL, model: DefaultModel,
		http: &http.Client{}, timeout: 30 * time.Second, maxRetries: 2,
		maxResponseBytes: 16 << 20,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("typesafe: nil client option")
		}
		option(c)
	}
	if c.apiKey == "" || strings.ContainsAny(c.apiKey, "\r\n") {
		return nil, fmt.Errorf("typesafe: a valid API key is required")
	}
	parsed, err := url.Parse(c.baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("typesafe: base URL must be an HTTP(S) root without credentials, query, or fragment")
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	if strings.TrimSpace(c.model) == "" || c.http == nil || c.timeout <= 0 || c.maxRetries < 0 || c.maxResponseBytes <= 0 || c.maxResponseBytes == int64(^uint64(0)>>1) {
		return nil, fmt.Errorf("typesafe: model, HTTP client, timeout, retry count, or response limit is invalid")
	}
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.http = &client
	return c, nil
}

// Evaluate asks a mixed batch of questions about one state. Every answer must
// match its submitted question's ID, kind, and options before it is returned.
func (c *Client) Evaluate(ctx context.Context, request Request) (Response, error) {
	if request.Model == "" {
		request.Model = c.model
	}
	body, questions, err := encodeRequest(request)
	if err != nil {
		return Response{}, err
	}
	data, requestID, err := c.do(ctx, http.MethodPost, "/v1/systemone", body)
	if err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.Unmarshal(data, &response); err != nil {
		var invalid *ResponseError
		if errors.As(err, &invalid) {
			return Response{}, invalid
		}
		// Unmarshal can reject invalid JSON before calling Response.UnmarshalJSON.
		return Response{}, &ResponseError{"malformed evaluation response"}
	}
	if err := response.validateQuestions(questions); err != nil {
		return Response{}, err
	}
	response.RequestID = requestID
	return response, nil
}

// Result preserves metadata alongside a statically typed single answer.
type Result[T Answer] struct {
	Answer    T
	Model     string
	Usage     Usage
	RequestID string
}

// Choose returns one selected option and the distribution across all options.
func (c *Client) Choose(ctx context.Context, state any, question Choice) (Result[ChoiceAnswer], error) {
	return evaluateOne[ChoiceAnswer](ctx, c, state, question)
}

// Score returns a probability-weighted position on the question's ordered rubric.
func (c *Client) Score(ctx context.Context, state any, question Score) (Result[ScoreAnswer], error) {
	return evaluateOne[ScoreAnswer](ctx, c, state, question)
}

// Check returns the probability of yes, without selecting a boolean threshold.
func (c *Client) Check(ctx context.Context, state any, question Noul) (Result[NoulAnswer], error) {
	return evaluateOne[NoulAnswer](ctx, c, state, question)
}

func evaluateOne[T Answer](ctx context.Context, c *Client, state any, question Question) (Result[T], error) {
	response, err := c.Evaluate(ctx, Request{State: state, Questions: map[string]Question{"answer": question}})
	if err != nil {
		return Result[T]{}, err
	}
	answer, err := answerAs[T](response, "answer", question.questionType())
	if err != nil {
		return Result[T]{}, err
	}
	return Result[T]{answer, response.Model, response.Usage, response.RequestID}, nil
}

type Model struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

type ModelsResponse struct {
	Models    []Model `json:"models"`
	RequestID string  `json:"-"`
}

// Models lists models advertised for the account. The service may also accept
// pinned model IDs that are not included in this list.
func (c *Client) Models(ctx context.Context) (ModelsResponse, error) {
	data, requestID, err := c.do(ctx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return ModelsResponse{}, err
	}
	var response ModelsResponse
	if json.Unmarshal(data, &response) != nil || response.Models == nil {
		return ModelsResponse{}, &ResponseError{"expected a models array"}
	}
	for _, model := range response.Models {
		if model.Name == "" {
			return ModelsResponse{}, &ResponseError{"model name is missing"}
		}
	}
	response.RequestID = requestID
	return response, nil
}
