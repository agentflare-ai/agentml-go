package stdin

import (
	"context"
	"fmt"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

func Loader() agentml.NamespaceLoader {
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		fmt.Println("DEBUG: Loader")
		return &Namespace{itp: itp}, nil
	}
}
