package openai

import (
	"context"
	"log/slog"
	"maps"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type Client struct {
	apiClient *openai.Client
	models    map[ModelName]*Model
}

type ClientOptions struct {
	APIKey  string
	BaseURL string
}

func NewClient(ctx context.Context, models map[ModelName]*Model, options *ClientOptions) (*Client, error) {
	if options == nil {
		options = &ClientOptions{}
	}

	// Initialize models map if nil
	if models == nil {
		models = make(map[ModelName]*Model)
	}

	// Build client options
	var opts []option.RequestOption
	if options.APIKey != "" {
		opts = append(opts, option.WithAPIKey(options.APIKey))
	}
	if options.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(options.BaseURL))
	}

	apiClient := openai.NewClient(opts...)

	return &Client{
		apiClient: apiClient,
		models:    maps.Clone(models),
	}, nil
}

func (c *Client) Chat(ctx context.Context, model ModelName, messages []openai.ChatCompletionMessageParamUnion, stream bool) (*openai.ChatCompletion, error) {
	maybeModel, ok := c.models[model]
	if !ok {
		// Dynamically register the model if not found
		slog.Debug("openai.client.chat: dynamically registering model", "model", model)
		maybeModel = &Model{
			Name:   string(model),
			Stream: stream,
		}
		c.models[model] = maybeModel
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.F(maybeModel.Name),
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
	maybeModel, ok := c.models[model]
	if !ok {
		// Dynamically register the model if not found
		slog.Debug("openai.client.chatWithTools: dynamically registering model", "model", model)
		maybeModel = &Model{
			Name:   string(model),
			Stream: stream,
		}
		c.models[model] = maybeModel
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.F(maybeModel.Name),
		Messages: openai.F(messages),
		Tools:    openai.F(tools),
	}

	if stream {
		// For streaming, we'll need a different approach
		// For now, we'll just use non-streaming
		slog.Warn("openai.client.chatWithTools: streaming not yet implemented, using non-streaming")
	}

	completion, err := c.apiClient.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}

	return completion, nil
}

// Deps holds dependencies for OpenAI executables
type Deps struct {
	Client *Client
}
