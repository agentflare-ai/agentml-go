package validate

import (
	"context"

	"github.com/agentflare-ai/agentml"
	"github.com/agentflare-ai/go-xmldom"
)

// Loader returns a NamespaceLoader for the validate namespace.
// This allows SCXML documents to validate .aml files or AML content.
//
// Usage in SCXML:
//
//	<scxml>
//	  <!-- Validate from file -->
//	  <validate xmlns="github.com/agentflare-ai/agentml-go/validate" src="agent.aml" location="validation_result" strict="true" recursive="false" />
//	  <!-- Validate from data model content -->
//	  <validate xmlns="github.com/agentflare-ai/agentml-go/validate" contentexpr="myAmlContent" location="validation_result" />
//	</scxml>
func Loader() agentml.NamespaceLoader {
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		return &Namespace{itp: itp}, nil
	}
}
