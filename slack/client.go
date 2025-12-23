package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/agentflare-ai/agentml-go/slack")

// Client handles Slack Web API interactions.
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a new Slack client using SLACK_BOT_TOKEN from environment.
func NewClient(ctx context.Context) (*Client, error) {
	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("SLACK_BOT_TOKEN environment variable is required")
	}

	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Close cleans up client resources.
func (c *Client) Close() error {
	// No resources to clean up currently
	return nil
}

// PostMessageRequest represents a Slack chat.postMessage API request.
type PostMessageRequest struct {
	Channel   string                 `json:"channel"`
	Text      string                 `json:"text,omitempty"`
	ThreadTS  string                 `json:"thread_ts,omitempty"`
	Blocks    []map[string]any       `json:"blocks,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	AsUser    bool                   `json:"as_user,omitempty"`
	Username  string                 `json:"username,omitempty"`
	IconEmoji string                 `json:"icon_emoji,omitempty"`
	IconURL   string                 `json:"icon_url,omitempty"`
}

// PostMessageResponse represents a Slack chat.postMessage API response.
type PostMessageResponse struct {
	OK      bool   `json:"ok"`
	Channel string `json:"channel,omitempty"`
	TS      string `json:"ts,omitempty"`
	Message struct {
		Text string `json:"text,omitempty"`
		User string `json:"user,omitempty"`
		TS   string `json:"ts,omitempty"`
	} `json:"message,omitempty"`
	Error string `json:"error,omitempty"`
}

// PostMessage sends a message to a Slack channel or user.
func (c *Client) PostMessage(ctx context.Context, req PostMessageRequest) (*PostMessageResponse, error) {
	ctx, span := tracer.Start(ctx, "slack.client.post_message",
		trace.WithAttributes(
			attribute.String("slack.channel", req.Channel),
			attribute.Bool("slack.has_thread_ts", req.ThreadTS != ""),
		))
	defer span.End()

	url := "https://slack.com/api/chat.postMessage"

	body, err := json.Marshal(req)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("slack: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("slack: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("slack: request failed: %w", err)
	}
	defer resp.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		span.RecordError(fmt.Errorf("slack: unexpected status %d: %s", resp.StatusCode, string(bodyBytes)))
		return nil, fmt.Errorf("slack: unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var apiResp PostMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("slack: failed to decode response: %w", err)
	}

	if !apiResp.OK {
		err := fmt.Errorf("slack API error: %s", apiResp.Error)
		span.RecordError(err)
		slog.WarnContext(ctx, "slack: API returned error",
			"error", apiResp.Error,
			"channel", req.Channel)
		return nil, err
	}

	span.SetAttributes(
		attribute.String("slack.message.ts", apiResp.TS),
		attribute.String("slack.message.channel", apiResp.Channel),
	)

	return &apiResp, nil
}
