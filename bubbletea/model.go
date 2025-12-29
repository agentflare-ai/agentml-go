package bubbletea

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type listAdapter struct {
	programID string
	config    listConfig

	cursor   int
	selected map[int]struct{}
}

func newListAdapter(programID string, cfg listConfig) *listAdapter {
	return &listAdapter{
		programID: programID,
		config:    cfg,
		selected:  make(map[int]struct{}),
	}
}

func (m *listAdapter) Type() string {
	return "list"
}

func (m *listAdapter) ID() string {
	return m.config.ID
}

func (m *listAdapter) Init() tea.Cmd {
	return nil
}

func (m *listAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	var flags updateFlags
	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "up", "k":
			if m.moveCursor(-1) {
				flags |= flagCursor
			}
		case "down", "j":
			if m.moveCursor(1) {
				flags |= flagCursor
			}
		case " ":
			if m.toggleSelection() {
				flags |= flagChanged
			}
		case "enter", "ctrl+m":
			m.ensureSelection()
			flags |= flagSubmitted
		}
	}
	return nil, flags
}

func (m *listAdapter) View() string {
	var b strings.Builder
	if title := m.config.Title; title != "" {
		fmt.Fprintf(&b, "%s\n\n", title)
	}
	for i, item := range m.config.Items {
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

func (m *listAdapter) moveCursor(delta int) bool {
	max := len(m.config.Items)
	if max == 0 {
		return false
	}
	next := m.cursor + delta
	if next < 0 {
		next = 0
	} else if next >= max {
		next = max - 1
	}
	if next == m.cursor {
		return false
	}
	m.cursor = next
	return true
}

func (m *listAdapter) toggleSelection() bool {
	if !m.config.Multi {
		m.selected = map[int]struct{}{m.cursor: {}}
		return true
	}
	if _, ok := m.selected[m.cursor]; ok {
		delete(m.selected, m.cursor)
	} else {
		m.selected[m.cursor] = struct{}{}
	}
	return true
}

func (m *listAdapter) ensureSelection() {
	if !m.config.Multi {
		m.selected = map[int]struct{}{m.cursor: {}}
		return
	}
	if len(m.selected) == 0 {
		m.selected[m.cursor] = struct{}{}
	}
}

func (m *listAdapter) Payload(reason string) map[string]any {
	return m.selectionPayload(reason)
}

func (m *listAdapter) CursorPayload() (map[string]any, bool) {
	if len(m.config.Items) == 0 {
		return nil, false
	}
	item := m.config.Items[m.cursor]
	payload := map[string]any{
		"component":   "list",
		"programId":   m.programID,
		"listId":      m.config.ID,
		"cursorIndex": m.cursor,
		"value":       effectiveValue(item),
		"label":       item.Label,
	}
	return payload, true
}

func (m *listAdapter) selectionPayload(reason string) map[string]any {
	indices := make([]int, 0, len(m.selected))
	for idx := range m.selected {
		indices = append(indices, idx)
	}
	if len(indices) == 0 && len(m.config.Items) > 0 {
		indices = append(indices, m.cursor)
	}
	sort.Ints(indices)

	values := make([]string, len(indices))
	labels := make([]string, len(indices))
	for i, idx := range indices {
		item := m.config.Items[idx]
		values[i] = effectiveValue(item)
		labels[i] = item.Label
	}

	payload := map[string]any{
		"component":       "list",
		"programId":       m.programID,
		"listId":          m.config.ID,
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
