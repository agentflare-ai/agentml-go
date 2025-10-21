package validate

import (
	"context"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
)

// Loader returns a NamespaceLoader for the validate namespace.
// This allows SCXML documents to validate AML content.
//
// Usage in SCXML:
//
//	<scxml xmlns:validate="github.com/agentflare-ai/agentml-go/validate">
//	  <!-- Validate inline content -->
//	  <validate:content content="&lt;agentml&gt;...&lt;/agentml&gt;" location="result" />
//	  <!-- Validate content from data model -->
//	  <validate:content contentexpr="generatedCode" location="result" strict="false" />
//	</scxml>
func Loader() agentml.NamespaceLoader {
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		return &Namespace{itp: itp}, nil
	}
}
