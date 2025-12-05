package bubbletea

import (
	"context"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

// NamespaceURI is the XML namespace for the Bubble Tea extension.
const NamespaceURI = "github.com/agentflare-ai/agentml-go/bubbletea"

// Deps wires external dependencies for the namespace.
type Deps struct {
	Manager *Manager
}

// Loader returns a NamespaceLoader that initializes the Bubble Tea namespace.
func Loader(deps *Deps) agentml.NamespaceLoader {
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		actual := deps
		if actual == nil {
			actual = &Deps{}
		}
		if actual.Manager == nil {
			actual.Manager = NewManager()
		}
		return &Namespace{
			itp:     itp,
			manager: actual.Manager,
		}, nil
	}
}
