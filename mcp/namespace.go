package mcp

import (
	"context"
	"fmt"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

// MCPNamespaceURI is the XML namespace URI used for MCP executable elements.
const MCPNamespaceURI = "github.com/agentflare-ai/agentml-go/mcp"

// Deps holds dependencies for MCP executables.
type Deps struct {
	ConnectionManager *ConnectionManager
}

// Loader returns a NamespaceLoader for the MCP namespace.
func Loader(deps *Deps) agentml.NamespaceLoader {
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		// Create a connection manager if not provided
		if deps == nil {
			deps = &Deps{
				ConnectionManager: NewConnectionManager(),
			}
		}
		if deps.ConnectionManager == nil {
			deps.ConnectionManager = NewConnectionManager()
		}

		return &ns{
			itp:  itp,
			deps: deps,
		}, nil
	}
}

type ns struct {
	itp  agentml.Interpreter
	deps *Deps
}

var _ agentml.Namespace = (*ns)(nil)

func (n *ns) URI() string {
	return MCPNamespaceURI
}

func (n *ns) Handle(ctx context.Context, el xmldom.Element) (bool, error) {
	if el == nil {
		return false, fmt.Errorf("mcp: element cannot be nil")
	}

	switch string(el.LocalName()) {
	case "connect":
		exec, err := NewConnect(ctx, el, n.deps.ConnectionManager)
		if err != nil {
			return true, err
		}
		return true, exec.Execute(ctx, n.itp)

	case "call":
		exec, err := NewCall(ctx, el, n.deps.ConnectionManager)
		if err != nil {
			return true, err
		}
		return true, exec.Execute(ctx, n.itp)

	case "get":
		exec, err := NewGet(ctx, el, n.deps.ConnectionManager)
		if err != nil {
			return true, err
		}
		return true, exec.Execute(ctx, n.itp)

	case "list":
		exec, err := NewList(ctx, el, n.deps.ConnectionManager)
		if err != nil {
			return true, err
		}
		return true, exec.Execute(ctx, n.itp)

	case "disconnect":
		exec, err := NewDisconnect(ctx, el, n.deps.ConnectionManager)
		if err != nil {
			return true, err
		}
		return true, exec.Execute(ctx, n.itp)

	default:
		return false, nil
	}
}

func (n *ns) Unload(ctx context.Context) error {
	// Close all connections when namespace is unloaded
	return n.deps.ConnectionManager.DisconnectAll(ctx)
}
