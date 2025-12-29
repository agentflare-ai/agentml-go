package bubbletea

import tea "github.com/charmbracelet/bubbletea"

func isEnterKey(msg tea.Msg) bool {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch key.String() {
	case "enter", "ctrl+m":
		return true
	default:
		return false
	}
}
