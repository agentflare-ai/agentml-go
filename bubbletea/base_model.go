package bubbletea

import (
	"context"
	"log/slog"
	"time"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-muid"
	tea "github.com/charmbracelet/bubbletea"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type eventDispatcher interface {
	Send(context.Context, *agentml.Event) error
}

type updateFlags uint8

const (
	flagChanged updateFlags = 1 << iota
	flagSubmitted
	flagCursor
)

type componentEvents struct {
	CursorEvent string
	ChangeEvent string
	SubmitEvent string
	QuitEvent   string
}

type componentAdapter interface {
	Type() string
	ID() string
	Init() tea.Cmd
	Update(msg tea.Msg) (tea.Cmd, updateFlags)
	View() string
	Payload(reason string) map[string]any
	CursorPayload() (map[string]any, bool)
}

type baseModel struct {
	ctx        context.Context
	dispatcher eventDispatcher
	programID  string
	adapter    componentAdapter
	events     componentEvents
}

func newBaseModel(ctx context.Context, programID string, adapter componentAdapter, events componentEvents, dispatcher eventDispatcher) *baseModel {
	return &baseModel{
		ctx:        ctx,
		dispatcher: dispatcher,
		programID:  programID,
		adapter:    adapter,
		events:     events,
	}
}

func (m *baseModel) Init() tea.Cmd {
	if m.adapter == nil {
		return nil
	}
	return m.adapter.Init()
}

func (m *baseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "ctrl+c", "q":
			if m.events.QuitEvent != "" {
				m.emitEvent(m.events.QuitEvent, m.adapter.Payload("quit"))
			}
			return m, tea.Quit
		}
	}

	cmd, flags := m.adapter.Update(msg)

	if flags&flagCursor != 0 && m.events.CursorEvent != "" {
		if payload, ok := m.adapter.CursorPayload(); ok {
			m.emitEvent(m.events.CursorEvent, payload)
		} else {
			m.emitEvent(m.events.CursorEvent, m.adapter.Payload("cursor"))
		}
	}
	if flags&flagChanged != 0 && m.events.ChangeEvent != "" {
		m.emitEvent(m.events.ChangeEvent, m.adapter.Payload("change"))
	}
	if flags&flagSubmitted != 0 {
		if m.events.SubmitEvent != "" {
			m.emitEvent(m.events.SubmitEvent, m.adapter.Payload("submit"))
		}
		if cmd != nil {
			return m, tea.Batch(cmd, tea.Quit)
		}
		return m, tea.Quit
	}

	return m, cmd
}

func (m *baseModel) View() string {
	if m.adapter == nil {
		return ""
	}
	return m.adapter.View()
}

func (m *baseModel) emitEvent(name string, data map[string]any) {
	if name == "" {
		return
	}

	ctx, span := tracer.Start(m.ctx, "bubbletea.event.emit",
		trace.WithAttributes(
			attribute.String("bubbletea.event.name", name),
			attribute.String("bubbletea.program.id", m.programID),
			attribute.String("bubbletea.component.type", m.adapter.Type()),
		))
	defer span.End()

	event := &agentml.Event{
		ID:        muid.MakeString(), // Derived from muid.String().
		Name:      name,
		Type:      agentml.EventTypeExternal,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}
	if err := m.dispatcher.Send(ctx, event); err != nil {
		slog.WarnContext(ctx, "bubbletea: failed to send event",
			"event", name,
			"error", err)
	}
}
