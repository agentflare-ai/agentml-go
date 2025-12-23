package slack

import (
	"context"
	"strings"
	"testing"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDataModel struct {
	store map[string]any
}

func newFakeDataModel() *fakeDataModel {
	return &fakeDataModel{store: make(map[string]any)}
}

func (f *fakeDataModel) Initialize(ctx context.Context, dataElements []agentml.Data) error {
	return nil
}

func (f *fakeDataModel) EvaluateValue(ctx context.Context, expression string) (any, error) {
	// Simple variable lookup
	if v, ok := f.store[expression]; ok {
		return v, nil
	}
	// Strip single quotes for simple string literals
	if strings.HasPrefix(expression, "'") && strings.HasSuffix(expression, "'") && len(expression) >= 2 {
		return expression[1 : len(expression)-1], nil
	}
	return expression, nil
}

func (f *fakeDataModel) EvaluateCondition(ctx context.Context, expression string) (bool, error) {
	return false, nil
}

func (f *fakeDataModel) EvaluateLocation(ctx context.Context, location string) (any, error) {
	return f.store[location], nil
}

func (f *fakeDataModel) Assign(ctx context.Context, location string, value any) error {
	f.store[location] = value
	return nil
}

func (f *fakeDataModel) GetVariable(ctx context.Context, id string) (any, error) {
	return f.store[id], nil
}

func (f *fakeDataModel) SetVariable(ctx context.Context, id string, value any) error {
	f.store[id] = value
	return nil
}

func (f *fakeDataModel) GetSystemVariable(ctx context.Context, name string) (any, error) {
	return nil, nil
}

func (f *fakeDataModel) SetSystemVariable(ctx context.Context, name string, value any) error {
	return nil
}

func (f *fakeDataModel) SetCurrentEvent(ctx context.Context, event any) error {
	return nil
}

func (f *fakeDataModel) ExecuteScript(ctx context.Context, script string) error {
	return nil
}

func (f *fakeDataModel) Clone(ctx context.Context) (agentml.DataModel, error) {
	return newFakeDataModel(), nil
}

func (f *fakeDataModel) ValidateExpression(ctx context.Context, expression string, exprType agentml.ExpressionType) error {
	return nil
}

type fakeInterpreter struct {
	dm        *fakeDataModel
	sentEvents []*agentml.Event
}

func newFakeInterpreter() *fakeInterpreter {
	return &fakeInterpreter{
		dm:        newFakeDataModel(),
		sentEvents: make([]*agentml.Event, 0),
	}
}

func (fi *fakeInterpreter) Send(ctx context.Context, event *agentml.Event) error {
	fi.sentEvents = append(fi.sentEvents, event)
	return nil
}

func (fi *fakeInterpreter) DataModel() agentml.DataModel {
	return fi.dm
}

// Implement other required methods with minimal implementations
func (fi *fakeInterpreter) Handle(ctx context.Context, event *agentml.Event) error { return nil }
func (fi *fakeInterpreter) Location(ctx context.Context) (string, error)           { return "", nil }
func (fi *fakeInterpreter) Type() string                                            { return "" }
func (fi *fakeInterpreter) Shutdown(ctx context.Context) error                      { return nil }
func (fi *fakeInterpreter) SessionID() string                                       { return "" }
func (fi *fakeInterpreter) Configuration() []string                                 { return nil }
func (fi *fakeInterpreter) In(ctx context.Context, stateId string) bool             { return false }
func (fi *fakeInterpreter) Raise(ctx context.Context, event *agentml.Event)         {}
func (fi *fakeInterpreter) Cancel(ctx context.Context, sendId string) error          { return nil }
func (fi *fakeInterpreter) Log(ctx context.Context, label, message string)           {}
func (fi *fakeInterpreter) Context() context.Context                                { return context.Background() }
func (fi *fakeInterpreter) Clock() agentml.Clock                                    { return nil }
func (fi *fakeInterpreter) ExecuteElement(ctx context.Context, element xmldom.Element) error {
	return nil
}
func (fi *fakeInterpreter) SendMessage(ctx context.Context, data agentml.SendData) error {
	return nil
}
func (fi *fakeInterpreter) ScheduleMessage(ctx context.Context, data agentml.SendData) (string, error) {
	return "", nil
}
func (fi *fakeInterpreter) InvokedSessions() map[string]agentml.Interpreter { return nil }
func (fi *fakeInterpreter) Tracer() agentml.Tracer                              { return nil }
func (fi *fakeInterpreter) Snapshot(ctx context.Context, maybeConfig ...agentml.SnapshotConfig) (xmldom.Document, error) {
	return nil, nil
}
func (fi *fakeInterpreter) Root() interface{} { return nil }
func (fi *fakeInterpreter) AfterFunc(ctx context.Context, fn func()) func() bool {
	return func() bool { return false }
}

type fakeClient struct {
	postMessageFunc func(ctx context.Context, req PostMessageRequest) (*PostMessageResponse, error)
}

func (fc *fakeClient) PostMessage(ctx context.Context, req PostMessageRequest) (*PostMessageResponse, error) {
	if fc.postMessageFunc != nil {
		return fc.postMessageFunc(ctx, req)
	}
	return &PostMessageResponse{
		OK:      true,
		Channel: req.Channel,
		TS:      "1234567890.123456",
	}, nil
}

func (fc *fakeClient) Close() error {
	return nil
}

func TestSendExecutable_MissingChannelAndUser(t *testing.T) {
	xml := `<send xmlns="` + NamespaceURI + `" />`
	dec := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := dec.Decode()
	require.NoError(t, err)
	el := doc.DocumentElement()

	client := &fakeClient{}
	exec, err := newSendExecutable(el, client)
	require.NoError(t, err)

	itp := newFakeInterpreter()
	err = exec.Execute(context.Background(), itp)

	assert.Error(t, err)
	platformErr, ok := err.(*agentml.PlatformError)
	require.True(t, ok)
	assert.Equal(t, "error.execution", platformErr.EventName)
	assert.Contains(t, platformErr.Message, "requires either channel or user")
}

func TestSendExecutable_WithChannelAndText(t *testing.T) {
	xml := `<send xmlns="` + NamespaceURI + `" channel="C1234567890" text="Hello, world!" />`
	dec := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := dec.Decode()
	require.NoError(t, err)
	el := doc.DocumentElement()

	client := &fakeClient{
		postMessageFunc: func(ctx context.Context, req PostMessageRequest) (*PostMessageResponse, error) {
			assert.Equal(t, "C1234567890", req.Channel)
			assert.Equal(t, "Hello, world!", req.Text)
			return &PostMessageResponse{
				OK:      true,
				Channel: req.Channel,
				TS:      "1234567890.123456",
			}, nil
		},
	}

	exec, err := newSendExecutable(el, client)
	require.NoError(t, err)

	itp := newFakeInterpreter()
	err = exec.Execute(context.Background(), itp)

	assert.NoError(t, err)
	assert.Len(t, itp.sentEvents, 1)
	assert.Equal(t, defaultSentEvent, itp.sentEvents[0].Name)
}

func TestSendExecutable_WithExpressions(t *testing.T) {
	xml := `<send xmlns="` + NamespaceURI + `" channel-expr="channelVar" text-expr="textVar" />`
	dec := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := dec.Decode()
	require.NoError(t, err)
	el := doc.DocumentElement()

	client := &fakeClient{
		postMessageFunc: func(ctx context.Context, req PostMessageRequest) (*PostMessageResponse, error) {
			assert.Equal(t, "C1234567890", req.Channel)
			assert.Equal(t, "Hello from expression!", req.Text)
			return &PostMessageResponse{
				OK:      true,
				Channel: req.Channel,
				TS:      "1234567890.123456",
			}, nil
		},
	}

	exec, err := newSendExecutable(el, client)
	require.NoError(t, err)

	itp := newFakeInterpreter()
	itp.dm.store["channelVar"] = "C1234567890"
	itp.dm.store["textVar"] = "Hello from expression!"

	err = exec.Execute(context.Background(), itp)

	assert.NoError(t, err)
}

func TestSendExecutable_CustomEvent(t *testing.T) {
	xml := `<send xmlns="` + NamespaceURI + `" channel="C1234567890" text="Test" event="custom.sent" />`
	dec := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := dec.Decode()
	require.NoError(t, err)
	el := doc.DocumentElement()

	client := &fakeClient{}
	exec, err := newSendExecutable(el, client)
	require.NoError(t, err)

	itp := newFakeInterpreter()
	err = exec.Execute(context.Background(), itp)

	assert.NoError(t, err)
	assert.Len(t, itp.sentEvents, 1)
	assert.Equal(t, "custom.sent", itp.sentEvents[0].Name)
}

