package bubbletea

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestListModelEmitsEvents(t *testing.T) {
	cfg := ProgramConfig{
		ProgramID: "p",
		List: listConfig{
			ID:          "choices",
			ChangeEvent: "ui.change",
			SubmitEvent: "ui.submit",
			QuitEvent:   "ui.quit",
			Items: []listItemConfig{
				{Label: "One"},
				{Label: "Two"},
			},
		},
	}

	dispatcher := newFakeDispatcher()
	model := newListModel(context.Background(), cfg, dispatcher)

	// Toggle selection should trigger change event.
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if len(dispatcher.events) == 0 || dispatcher.events[0].Name != "ui.change" {
		t.Fatalf("expected change event, got %+v", dispatcher.events)
	}

	// Submit key should emit submit event and quit command.
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit command on submit")
	}
	if msg := cmd(); msg != nil {
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Fatalf("expected quit msg, got %#v", msg)
		}
	}

	if len(dispatcher.events) < 2 || dispatcher.events[len(dispatcher.events)-1].Name != "ui.submit" {
		t.Fatalf("expected submit event, got %+v", dispatcher.events)
	}

	// Quit should emit quit event.
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if len(dispatcher.events) < 3 || dispatcher.events[len(dispatcher.events)-1].Name != "ui.quit" {
		t.Fatalf("expected quit event, got %+v", dispatcher.events)
	}
}
