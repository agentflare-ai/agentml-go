package openai

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

// Loader returns a NamespaceLoader for the OpenAI namespace.
// It closes over DI deps (OpenAI client) and the interpreter.
func Loader() agentml.NamespaceLoader {
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		client, err := NewClient(ctx, &ClientOptions{
			APIKey:  os.Getenv("OPENAI_API_KEY"),
			BaseURL: os.Getenv("OPENAI_BASE_URL"),
		})
		if err != nil {
			return nil, err
		}
		baseURL := os.Getenv("OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "default"
		}
		slog.Info("openai: client created", "baseURL", baseURL, "client", client)
		return &ns{itp: itp, client: client}, nil
	}
}

type ns struct {
	itp    agentml.Interpreter
	client *Client
}

var _ agentml.Namespace = (*ns)(nil)

func (n *ns) URI() string { return OpenAINamespaceURI }

func (n *ns) Unload(ctx context.Context) error { return nil }

func (n *ns) Handle(ctx context.Context, el xmldom.Element) (bool, error) {
	if el == nil {
		return false, fmt.Errorf("openai: element cannot be nil")
	}
	switch string(el.LocalName()) {
	case "generate":
		slog.Info("openai: handle generate", "el", el)
		return true, n.handleGenerate(ctx, el)
	default:
		return false, nil
	}
}

func (n *ns) handleGenerate(ctx context.Context, el xmldom.Element) error {
	exec, err := NewGenerate(ctx, el)
	if err != nil {
		return err
	}
	g := exec.(*Generate)
	g.SetClient(n.client)
	return g.Execute(ctx, n.itp)
}
