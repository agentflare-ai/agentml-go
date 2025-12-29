package bubbletea

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

// Namespace implements agentml.Namespace for Bubble Tea executable content.
type Namespace struct {
	itp     agentml.Interpreter
	manager *Manager
}

var _ agentml.Namespace = (*Namespace)(nil)

func (n *Namespace) URI() string { return NamespaceURI }

func (n *Namespace) Unload(ctx context.Context) error { return nil }

// Handle executes Bubble Tea executables.
func (n *Namespace) Handle(ctx context.Context, el xmldom.Element) (bool, error) {
	if el == nil {
		return false, fmt.Errorf("bubbletea: element cannot be nil")
	}

	switch strings.ToLower(string(el.LocalName())) {
	case "program":
		exec, err := newProgramExecutable(ctx, el, n.manager, n.itp)
		if err != nil {
			return true, err
		}
		return true, exec.Execute(ctx, n.itp)
	default:
		return false, nil
	}
}
