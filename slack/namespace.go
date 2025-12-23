package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

// NamespaceURI is the XML namespace for the Slack extension.
const NamespaceURI = "github.com/agentflare-ai/agentml-go/slack"

// Namespace implements agentml.Namespace for Slack executable content.
type Namespace struct {
	itp    agentml.Interpreter
	client ClientInterface
}

var _ agentml.Namespace = (*Namespace)(nil)

func (n *Namespace) URI() string { return NamespaceURI }

func (n *Namespace) Unload(ctx context.Context) error {
	if n.client != nil {
		return n.client.Close()
	}
	return nil
}

// Handle executes Slack executables.
func (n *Namespace) Handle(ctx context.Context, el xmldom.Element) (bool, error) {
	if el == nil {
		return false, fmt.Errorf("slack: element cannot be nil")
	}

	switch strings.ToLower(string(el.LocalName())) {
	case "send":
		exec, err := newSendExecutable(el, n.client)
		if err != nil {
			return true, err
		}
		return true, exec.Execute(ctx, n.itp)
	default:
		return false, nil
	}
}

// Deps wires external dependencies for the namespace.
type Deps struct {
	Client ClientInterface
}

// Loader returns a NamespaceLoader that initializes the Slack namespace.
func Loader(maybeDeps ...Deps) agentml.NamespaceLoader {
	var deps *Deps
	if len(maybeDeps) > 0 {
		deps = &maybeDeps[0]
	}
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		actual := deps
		if actual == nil {
			actual = &Deps{}
		}
		if actual.Client == nil {
			client, err := NewClient(ctx)
			if err != nil {
				return nil, fmt.Errorf("slack: failed to create client: %w", err)
			}
			actual.Client = client
		}
		return &Namespace{
			itp:    itp,
			client: actual.Client,
		}, nil
	}
}
