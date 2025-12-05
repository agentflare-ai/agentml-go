package bubbletea

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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

type listModel struct {
	ctx        context.Context
	dispatcher eventDispatcher
	config     ProgramConfig

	cursor   int
	selected map[int]struct{}
}

func newListModel(ctx context.Context, cfg ProgramConfig, dispatcher eventDispatcher) *listModel {
	return &listModel{
		ctx:        ctx,
		dispatcher: dispatcher,
		config:     cfg,
		selected:   make(map[int]struct{}),
	}
}

func (m *listModel) Init() tea.Cmd {
	return nil
}

func (m *listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "ctrl+c", "q":
			m.emitSelectionEvent(m.config.List.QuitEvent, "quit")
			return m, tea.Quit
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case " ":
			m.toggleSelection()
		case "enter":
			if !m.config.List.Multi {
				m.selected = map[int]struct{}{m.cursor: {}}
			} else if len(m.selected) == 0 {
				m.selected[m.cursor] = struct{}{}
			}
			m.emitSelectionEvent(m.config.List.SubmitEvent, "submit")
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *listModel) View() string {
	var b strings.Builder
	if title := m.config.List.Title; title != "" {
		fmt.Fprintf(&b, "%s\n\n", title)
	}
	for i, item := range m.config.List.Items {
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}
		check := " "
		if _, ok := m.selected[i]; ok {
			check = "x"
		}
		fmt.Fprintf(&b, "%s [%s] %s\n", cursor, check, item.Label)
	}
	b.WriteString("\nPress q to quit.\n")
	return b.String()
}

func (m *listModel) moveCursor(delta int) {
	max := len(m.config.List.Items)
	if max == 0 {
		return
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	} else if next >= max {
		next = max - 1
	}
	if next == m.cursor {
		return
	}
	m.cursor = next
	if name := m.config.List.CursorEvent; name != "" {
		item := m.config.List.Items[m.cursor]
		payload := map[string]any{
			"component":   "list",
			"programId":   m.config.ProgramID,
			"listId":      m.config.List.ID,
			"cursorIndex": m.cursor,
			"value":       effectiveValue(item),
			"label":       item.Label,
		}
		m.dispatchEvent(name, payload)
	}
}

func (m *listModel) toggleSelection() {
	if !m.config.List.Multi {
		m.selected = map[int]struct{}{m.cursor: {}}
		m.emitSelectionEvent(m.config.List.ChangeEvent, "change")
		return
	}
	if _, ok := m.selected[m.cursor]; ok {
		delete(m.selected, m.cursor)
	} else {
		m.selected[m.cursor] = struct{}{}
	}
	m.emitSelectionEvent(m.config.List.ChangeEvent, "change")
}

func (m *listModel) emitSelectionEvent(eventName, reason string) {
	if eventName == "" {
		return
	}
	m.dispatchEvent(eventName, m.selectionPayload(reason))
}

func (m *listModel) dispatchEvent(name string, data map[string]any) {
	ctx, span := tracer.Start(m.ctx, "bubbletea.event.emit",
		trace.WithAttributes(
			attribute.String("bubbletea.event.name", name),
			attribute.String("bubbletea.program.id", m.config.ProgramID),
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

func (m *listModel) selectionPayload(reason string) map[string]any {
	indices := make([]int, 0, len(m.selected))
	for idx := range m.selected {
		indices = append(indices, idx)
	}
	if len(indices) == 0 && len(m.config.List.Items) > 0 {
		indices = append(indices, m.cursor)
	}
	sort.Ints(indices)

	values := make([]string, len(indices))
	labels := make([]string, len(indices))
	for i, idx := range indices {
		item := m.config.List.Items[idx]
		values[i] = effectiveValue(item)
		labels[i] = item.Label
	}

	payload := map[string]any{
		"component":       "list",
		"programId":       m.config.ProgramID,
		"listId":          m.config.List.ID,
		"selectedIndices": indices,
		"selectedValues":  values,
		"selectedLabels":  labels,
		"reason":          reason,
	}
	return payload
}

func effectiveValue(item listItemConfig) string {
	if item.Value != "" {
		return item.Value
	}
	return item.Label
}
