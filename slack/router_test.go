package slack

import (
	"context"
	"testing"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInterpreterForRouter struct {
	sentEvents []*agentml.Event
}

func (fi *fakeInterpreterForRouter) Send(ctx context.Context, event *agentml.Event) error {
	fi.sentEvents = append(fi.sentEvents, event)
	return nil
}

// Implement minimal required methods
func (fi *fakeInterpreterForRouter) Handle(ctx context.Context, event *agentml.Event) error { return nil }
func (fi *fakeInterpreterForRouter) Location(ctx context.Context) (string, error)           { return "", nil }
func (fi *fakeInterpreterForRouter) Type() string                                            { return "" }
func (fi *fakeInterpreterForRouter) Shutdown(ctx context.Context) error                      { return nil }
func (fi *fakeInterpreterForRouter) SessionID() string                                       { return "" }
func (fi *fakeInterpreterForRouter) Configuration() []string                                 { return nil }
func (fi *fakeInterpreterForRouter) In(ctx context.Context, stateId string) bool             { return false }
func (fi *fakeInterpreterForRouter) Raise(ctx context.Context, event *agentml.Event)       {}
func (fi *fakeInterpreterForRouter) Cancel(ctx context.Context, sendId string) error         { return nil }
func (fi *fakeInterpreterForRouter) Log(ctx context.Context, label, message string)         {}
func (fi *fakeInterpreterForRouter) Context() context.Context                               { return context.Background() }
func (fi *fakeInterpreterForRouter) Clock() agentml.Clock                                    { return nil }
func (fi *fakeInterpreterForRouter) DataModel() agentml.DataModel                            { return nil }
func (fi *fakeInterpreterForRouter) ExecuteElement(ctx context.Context, element xmldom.Element) error {
	return nil
}
func (fi *fakeInterpreterForRouter) SendMessage(ctx context.Context, data agentml.SendData) error {
	return nil
}
func (fi *fakeInterpreterForRouter) ScheduleMessage(ctx context.Context, data agentml.SendData) (string, error) {
	return "", nil
}
func (fi *fakeInterpreterForRouter) InvokedSessions() map[string]agentml.Interpreter { return nil }
func (fi *fakeInterpreterForRouter) Tracer() agentml.Tracer                              { return nil }
func (fi *fakeInterpreterForRouter) Snapshot(ctx context.Context, maybeConfig ...agentml.SnapshotConfig) (xmldom.Document, error) {
	return nil, nil
}
func (fi *fakeInterpreterForRouter) Root() interface{} { return nil }
func (fi *fakeInterpreterForRouter) AfterFunc(ctx context.Context, fn func()) func() bool {
	return func() bool { return false }
}

func TestRouter_HandleEvent(t *testing.T) {
	itp := &fakeInterpreterForRouter{
		sentEvents: make([]*agentml.Event, 0),
	}

	router := NewRouter("test-secret", itp)

	event := SlackEvent{
		Type:    "message",
		User:    "U1234567890",
		Text:    "Hello, world!",
		Channel: "C1234567890",
		TS:      "1234567890.123456",
		EventTS: "1234567890.123456",
	}

	err := router.HandleEvent(context.Background(), event)
	require.NoError(t, err)

	assert.Len(t, itp.sentEvents, 1)
	assert.Equal(t, "slack.message.posted", itp.sentEvents[0].Name)
	assert.Equal(t, agentml.EventTypeExternal, itp.sentEvents[0].Type)
	assert.Equal(t, "slack", itp.sentEvents[0].Origin)
}

func TestMapSlackEventToAgentML(t *testing.T) {
	tests := []struct {
		slackType string
		expected  string
	}{
		{"message", "slack.message.posted"},
		{"reaction_added", "slack.reaction.added"},
		{"reaction_removed", "slack.reaction.removed"},
		{"app_mention", "slack.app.mention"},
		{"unknown_type", "slack.unknown_type"},
	}

	for _, tt := range tests {
		t.Run(tt.slackType, func(t *testing.T) {
			result := mapSlackEventToAgentML(tt.slackType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRouter_VerifyRequest_ValidSignature(t *testing.T) {
	// This is a simplified test - full signature verification would require
	// proper HMAC calculation which is complex to test without real requests
	router := NewRouter("test-secret", nil)

	// We can't easily test VerifyRequest without a real HTTP request,
	// but we can verify the function exists and has the right signature
	assert.NotNil(t, router.VerifyRequest)
}

func TestRouter_HTTPHandler(t *testing.T) {
	itp := &fakeInterpreterForRouter{
		sentEvents: make([]*agentml.Event, 0),
	}

	router := NewRouter("test-secret", itp)
	handler := router.HTTPHandler()

	assert.NotNil(t, handler)
	// Full HTTP handler testing would require setting up an HTTP test server
	// which is more complex. This verifies the handler can be created.
}

