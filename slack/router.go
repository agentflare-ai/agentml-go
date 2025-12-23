package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-muid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Router handles incoming Slack events and converts them to AgentML events.
// This is designed to be integrated into an HTTP server by host applications.
type Router struct {
	signingSecret string
	interpreter   agentml.Interpreter
}

// NewRouter creates a new Slack event router.
func NewRouter(signingSecret string, itp agentml.Interpreter) *Router {
	return &Router{
		signingSecret: signingSecret,
		interpreter:   itp,
	}
}

// HandleEvent processes a Slack Events API event and converts it to an AgentML event.
func (r *Router) HandleEvent(ctx context.Context, event SlackEvent) error {
	ctx, span := tracer.Start(ctx, "slack.router.handle_event",
		trace.WithAttributes(
			attribute.String("slack.event.type", event.Type),
		))
	defer span.End()

	// Map Slack event type to AgentML event name
	eventName := mapSlackEventToAgentML(event.Type)

	// Build event data payload
	eventData := map[string]any{
		"component": "slack",
		"slack": map[string]any{
			"type":     event.Type,
			"event_ts": event.EventTS,
			"user":     event.User,
			"text":     event.Text,
			"channel":  event.Channel,
			"ts":       event.TS,
		},
	}

	// Add event-specific fields
	if event.Channel != "" {
		eventData["channel"] = event.Channel
	}
	if event.User != "" {
		eventData["user"] = event.User
	}
	if event.Text != "" {
		eventData["text"] = event.Text
	}
	if event.TS != "" {
		eventData["ts"] = event.TS
	}

	agentmlEvent := &agentml.Event{
		ID:         muid.MakeString(), // Derived from muid.String().
		Name:       eventName,
		Type:       agentml.EventTypeExternal,
		Timestamp:  time.Now().UTC(),
		Data:       eventData,
		Origin:     "slack",
		OriginType: "github.com/agentflare-ai/agentml-go/slack",
	}

	if err := r.interpreter.Send(ctx, agentmlEvent); err != nil {
		span.RecordError(err)
		return fmt.Errorf("slack: failed to send event to interpreter: %w", err)
	}

	return nil
}

// VerifyRequest verifies a Slack request signature.
func (r *Router) VerifyRequest(req *http.Request, body []byte) bool {
	signature := req.Header.Get("X-Slack-Signature")
	if signature == "" {
		return false
	}

	timestamp := req.Header.Get("X-Slack-Request-Timestamp")
	if timestamp == "" {
		return false
	}

	// Check timestamp (reject if older than 5 minutes)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix()-ts > 300 {
		return false
	}

	// Build signature base string
	sigBase := fmt.Sprintf("v0:%s:%s", timestamp, string(body))

	// Compute HMAC
	mac := hmac.New(sha256.New, []byte(r.signingSecret))
	mac.Write([]byte(sigBase))
	expectedSig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// HTTPHandler returns an http.Handler for processing Slack Events API requests.
func (r *Router) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		// Read body
		body, err := io.ReadAll(req.Body)
		if err != nil {
			slog.WarnContext(ctx, "slack: failed to read request body", "error", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Verify signature
		if !r.VerifyRequest(req, body) {
			slog.WarnContext(ctx, "slack: invalid request signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse Slack event
		var slackEvent SlackEvent
		if err := json.Unmarshal(body, &slackEvent); err != nil {
			slog.WarnContext(ctx, "slack: failed to parse event", "error", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Handle event
		if err := r.HandleEvent(ctx, slackEvent); err != nil {
			slog.ErrorContext(ctx, "slack: failed to handle event", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
}

// SlackEvent represents a Slack Events API event payload.
type SlackEvent struct {
	Type    string `json:"type"`
	EventTS string `json:"event_ts,omitempty"`
	User    string `json:"user,omitempty"`
	Text    string `json:"text,omitempty"`
	Channel string `json:"channel,omitempty"`
	TS      string `json:"ts,omitempty"`
	// Add other fields as needed
}

// mapSlackEventToAgentML maps Slack event types to AgentML event names.
func mapSlackEventToAgentML(slackType string) string {
	// Convert Slack event types to AgentML event names
	// e.g., "message" -> "slack.message.posted"
	prefix := "slack."
	suffix := strings.ReplaceAll(strings.ToLower(slackType), ".", "_")

	switch suffix {
	case "message":
		return prefix + "message.posted"
	case "reaction_added":
		return prefix + "reaction.added"
	case "reaction_removed":
		return prefix + "reaction.removed"
	case "app_mention":
		return prefix + "app.mention"
	default:
		return prefix + suffix
	}
}
