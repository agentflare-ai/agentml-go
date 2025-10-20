package openai

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type Client struct {
	apiClient *openai.Client
}

type ClientOptions struct {
	APIKey  string
	BaseURL string
}

// customTransport wraps http.Transport to rewrite URLs for custom OpenAI-compatible endpoints
type customTransport struct {
	baseTransport http.RoundTripper
	baseURL       string
}

func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only rewrite if we have a custom base URL configured
	if t.baseURL == "" {
		return t.baseTransport.RoundTrip(req)
	}

	// Parse the configured base URL
	baseURL, err := url.Parse(t.baseURL)
	if err != nil {
		slog.Warn("Failed to parse custom base URL", "baseURL", t.baseURL, "error", err)
		return t.baseTransport.RoundTrip(req)
	}

	// If the request is going to api.openai.com, rewrite it to use our custom endpoint
	if strings.Contains(req.URL.Host, "api.openai.com") {
		req.URL.Scheme = baseURL.Scheme
		req.URL.Host = baseURL.Host
		// Keep the path as-is (WithBaseURL should handle the base path)
		slog.Debug("customTransport: rewrote OpenAI URL", "original", req.URL.String(), "baseURL", t.baseURL)
	}

	return t.baseTransport.RoundTrip(req)
}

func NewClient(ctx context.Context, options *ClientOptions) (*Client, error) {
	if options == nil {
		options = &ClientOptions{}
	}

	// Create HTTP client with reasonable timeouts and URL rewriting
	baseTransport := &http.Transport{
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Wrap transport to rewrite URLs for custom OpenAI-compatible endpoints
	var transport http.RoundTripper = baseTransport
	if options.BaseURL != "" && !strings.Contains(options.BaseURL, "api.openai.com") {
		slog.Info("Creating customTransport for baseURL", "baseURL", options.BaseURL)
		transport = &customTransport{
			baseTransport: baseTransport,
			baseURL:       options.BaseURL,
		}
	}

	httpClient := &http.Client{
		Timeout:   90 * time.Second,
		Transport: transport,
	}

	// Build client options
	var opts []option.RequestOption
	opts = append(opts, option.WithHTTPClient(httpClient))
	if options.APIKey != "" {
		opts = append(opts, option.WithAPIKey(options.APIKey))
	}
	// Note: BaseURL is handled by our custom transport, not WithBaseURL

	apiClient := openai.NewClient(opts...)
	return &Client{
		apiClient: apiClient,
	}, nil
}

func (c *Client) Chat(ctx context.Context, model ModelName, messages []openai.ChatCompletionMessageParamUnion, stream bool) (*openai.ChatCompletion, error) {
	// Add a 120-second timeout for complex AgentML generation
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	params := openai.ChatCompletionNewParams{
		Model:    openai.F(string(model)),
		Messages: openai.F(messages),
	}

	if stream {
		// For streaming, we'll need a different approach
		// For now, we'll just use non-streaming
		slog.Warn("openai.client.chat: streaming not yet implemented, using non-streaming")
	}

	completion, err := c.apiClient.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}

	return completion, nil
}

func (c *Client) ChatWithTools(ctx context.Context, model ModelName, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolParam, stream bool) (*openai.ChatCompletion, error) {
	// Add a 120-second timeout for complex AgentML generation
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	params := openai.ChatCompletionNewParams{
		Model:    openai.F(string(model)),
		Messages: openai.F(messages),
		Tools:    openai.F(tools),
		// Force the model to call at least one tool (reject free text responses)
		ToolChoice: openai.F[openai.ChatCompletionToolChoiceOptionUnionParam](
			openai.ChatCompletionToolChoiceOptionBehavior("required"),
		),
	}

	if stream {
		// For streaming, we'll need a different approach
		// For now, we'll just use non-streaming
		slog.Warn("openai.client.chatWithTools: streaming not yet implemented, using non-streaming")
	}

	slog.Info("openai.client.chatWithTools: calling OpenAI API", "model", model, "num_tools", len(tools))
	completion, err := c.apiClient.Chat.Completions.New(ctx, params)
	if err != nil {
		slog.Error("openai.client.chatWithTools: API call failed", "error", err)
		return nil, err
	}
	slog.Info("openai.client.chatWithTools: API call succeeded")

	return completion, nil
}

// Deps holds dependencies for OpenAI executables
type Deps struct {
	Client *Client
}
