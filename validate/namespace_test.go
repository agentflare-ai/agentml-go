package validate

import (
	"context"
	"testing"

	"github.com/agentflare-ai/agentmlx/validator"
)

func TestNamespace_URI(t *testing.T) {
	// Test that the namespace can be created and has correct URI
	ns := &Namespace{}
	if uri := ns.URI(); uri != NamespaceURI {
		t.Errorf("expected URI %q, got %q", NamespaceURI, uri)
	}
	expectedURI := "github.com/agentflare-ai/agentml-go/validate"
	if uri := ns.URI(); uri != expectedURI {
		t.Errorf("expected URI %q, got %q", expectedURI, uri)
	}
}

func TestNamespace_Unload(t *testing.T) {
	ns := &Namespace{}
	if err := ns.Unload(context.TODO()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidationResult_Struct(t *testing.T) {
	// Test that ValidationResult struct can be created and has expected fields
	result := ValidationResult{
		Valid:        true,
		ErrorCount:   0,
		WarningCount: 1,
		InfoCount:    2,
		Diagnostics:  []validator.Diagnostic{},
	}

	if !result.Valid {
		t.Errorf("expected Valid to be true")
	}
	if result.ErrorCount != 0 {
		t.Errorf("expected ErrorCount to be 0, got %d", result.ErrorCount)
	}
	if result.WarningCount != 1 {
		t.Errorf("expected WarningCount to be 1, got %d", result.WarningCount)
	}
	if result.InfoCount != 2 {
		t.Errorf("expected InfoCount to be 2, got %d", result.InfoCount)
	}
	if len(result.Diagnostics) != 0 {
		t.Errorf("expected Diagnostics to be empty, got %v", result.Diagnostics)
	}
}
