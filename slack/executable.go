package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-muid"
	"github.com/agentflare-ai/go-xmldom"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultSentEvent = "slack.message.sent"
)

// ClientInterface defines the interface for Slack client operations.
type ClientInterface interface {
	PostMessage(ctx context.Context, req PostMessageRequest) (*PostMessageResponse, error)
	Close() error
}

type sendExecutable struct {
	element xmldom.Element
	client  ClientInterface
	config  SendConfig
}

// SendConfig captures the declarative instructions for sending a Slack message.
type SendConfig struct {
	Channel     string
	ChannelExpr string
	User        string
	UserExpr    string
	Text        string
	TextExpr    string
	ThreadTS    string
	ThreadExpr  string
	BlocksExpr  string
	Event       string
}

func newSendExecutable(el xmldom.Element, client ClientInterface) (*sendExecutable, error) {
	cfg, err := parseSendConfig(el)
	if err != nil {
		return nil, err
	}
	return &sendExecutable{
		element: el,
		client:  client,
		config:  cfg,
	}, nil
}

func (s *sendExecutable) Execute(ctx context.Context, itp agentml.Interpreter) error {
	ctx, span := tracer.Start(ctx, "slack.send.execute",
		trace.WithAttributes(
			attribute.String("slack.send.channel", s.config.Channel),
			attribute.String("slack.send.channel_expr", s.config.ChannelExpr),
		))
	defer span.End()

	dm := itp.DataModel()
	if dm == nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "slack:send requires a data model",
			Data: map[string]any{
				"element": "slack:send",
			},
		}
	}

	// Evaluate channel
	channel := s.config.Channel
	if channel == "" && s.config.ChannelExpr != "" {
		val, err := dm.EvaluateValue(ctx, s.config.ChannelExpr)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("slack:send failed to evaluate channel expression: %v", err),
				Data: map[string]any{
					"element": "slack:send",
					"expr":    s.config.ChannelExpr,
				},
				Cause: err,
			}
		}
		if str, ok := val.(string); ok {
			channel = str
		} else {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   "slack:send channel expression must evaluate to a string",
				Data: map[string]any{
					"element": "slack:send",
					"expr":    s.config.ChannelExpr,
					"value":   val,
				},
			}
		}
	}

	// Evaluate user (alternative to channel)
	user := s.config.User
	if user == "" && s.config.UserExpr != "" {
		val, err := dm.EvaluateValue(ctx, s.config.UserExpr)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("slack:send failed to evaluate user expression: %v", err),
				Data: map[string]any{
					"element": "slack:send",
					"expr":    s.config.UserExpr,
				},
				Cause: err,
			}
		}
		if str, ok := val.(string); ok {
			user = str
		} else {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   "slack:send user expression must evaluate to a string",
				Data: map[string]any{
					"element": "slack:send",
					"expr":    s.config.UserExpr,
					"value":   val,
				},
			}
		}
	}

	// Determine target: channel or user
	target := channel
	if target == "" && user != "" {
		target = user
	}
	if target == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "slack:send requires either channel or user attribute/expression",
			Data: map[string]any{
				"element": "slack:send",
			},
		}
	}

	// Evaluate text
	text := s.config.Text
	if text == "" && s.config.TextExpr != "" {
		val, err := dm.EvaluateValue(ctx, s.config.TextExpr)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("slack:send failed to evaluate text expression: %v", err),
				Data: map[string]any{
					"element": "slack:send",
					"expr":    s.config.TextExpr,
				},
				Cause: err,
			}
		}
		if str, ok := val.(string); ok {
			text = str
		} else {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   "slack:send text expression must evaluate to a string",
				Data: map[string]any{
					"element": "slack:send",
					"expr":    s.config.TextExpr,
					"value":   val,
				},
			}
		}
	}

	// Evaluate thread_ts
	threadTS := s.config.ThreadTS
	if threadTS == "" && s.config.ThreadExpr != "" {
		val, err := dm.EvaluateValue(ctx, s.config.ThreadExpr)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("slack:send failed to evaluate thread_ts expression: %v", err),
				Data: map[string]any{
					"element": "slack:send",
					"expr":    s.config.ThreadExpr,
				},
				Cause: err,
			}
		}
		if str, ok := val.(string); ok {
			threadTS = str
		}
	}

	// Evaluate blocks (optional)
	var blocks []map[string]any
	if s.config.BlocksExpr != "" {
		val, err := dm.EvaluateValue(ctx, s.config.BlocksExpr)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("slack:send failed to evaluate blocks expression: %v", err),
				Data: map[string]any{
					"element": "slack:send",
					"expr":    s.config.BlocksExpr,
				},
				Cause: err,
			}
		}
		// Try to convert to blocks format
		if blocksList, ok := val.([]any); ok {
			for _, item := range blocksList {
				if block, ok := item.(map[string]any); ok {
					blocks = append(blocks, block)
				}
			}
		} else if blockMap, ok := val.(map[string]any); ok {
			// Single block
			blocks = []map[string]any{blockMap}
		}
	}

	// Build request
	req := PostMessageRequest{
		Channel:  target,
		Text:     text,
		ThreadTS: threadTS,
		Blocks:   blocks,
	}

	// Send message
	resp, err := s.client.PostMessage(ctx, req)
	if err != nil {
		return &agentml.PlatformError{
			EventName: "error.communication",
			Message:   fmt.Sprintf("slack:send failed to post message: %v", err),
			Data: map[string]any{
				"element": "slack:send",
				"channel": target,
			},
			Cause: err,
		}
	}

	// Emit completion event if configured
	eventName := s.config.Event
	if eventName == "" {
		eventName = defaultSentEvent
	}

	eventData := map[string]any{
		"component": "slack",
		"action":    "send",
		"channel":   resp.Channel,
		"ts":        resp.TS,
		"ok":        resp.OK,
	}

	event := &agentml.Event{
		ID:        muid.MakeString(), // Derived from muid.String().
		Name:      eventName,
		Type:      agentml.EventTypeExternal,
		Timestamp: time.Now().UTC(),
		Data:      eventData,
	}

	if err := itp.Send(ctx, event); err != nil {
		slog.WarnContext(ctx, "slack: failed to send completion event",
			"event", eventName,
			"error", err)
	}

	return nil
}

func parseSendConfig(el xmldom.Element) (SendConfig, error) {
	cfg := SendConfig{
		Channel:     strings.TrimSpace(string(el.GetAttribute("channel"))),
		User:        strings.TrimSpace(string(el.GetAttribute("user"))),
		Text:        strings.TrimSpace(string(el.GetAttribute("text"))),
		ThreadTS:    strings.TrimSpace(string(el.GetAttribute("thread_ts"))),
		ChannelExpr: strings.TrimSpace(string(el.GetAttribute("channel-expr"))),
		UserExpr:    strings.TrimSpace(string(el.GetAttribute("user-expr"))),
		TextExpr:    strings.TrimSpace(string(el.GetAttribute("text-expr"))),
		ThreadExpr:  strings.TrimSpace(string(el.GetAttribute("thread-expr"))),
		BlocksExpr:  strings.TrimSpace(string(el.GetAttribute("blocks-expr"))),
		Event:       strings.TrimSpace(string(el.GetAttribute("event"))),
	}

	return cfg, nil
}
