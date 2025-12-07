package validator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentflare-ai/go-xmldom"
)

// parseXML is a test helper to parse XML strings
func parseXML(xml string) (xmldom.Document, error) {
	dec := xmldom.NewDecoder(strings.NewReader(xml))
	return dec.Decode()
}

// TestExtractSchemaDeclarations tests extracting schema:* attributes from root element
func TestExtractSchemaDeclarations(t *testing.T) {
	// schema:* attributes must be in the http://agentflare.ai/agentml/schema namespace
	scxmlDoc := `<?xml version="1.0" encoding="UTF-8"?>
<agentml xmlns="github.com/agentflare-ai/agentml"
         xmlns:schema="http://agentflare.ai/agentml/schema"
         version="1.0"
         schema:user="file://user-schema.json"
         schema:api="github.com/company/api-schema.json">
  <state id="main"/>
</agentml>`

	doc, err := parseXML(scxmlDoc)
	if err != nil {
		t.Fatalf("Failed to parse XML: %v", err)
	}

	root := doc.DocumentElement()
	declarations, err := ExtractSchemaDeclarations(root)
	if err != nil {
		t.Fatalf("Failed to extract declarations: %v", err)
	}

	if len(declarations) != 2 {
		t.Errorf("Expected 2 declarations, got %d", len(declarations))
	}

	if declarations["user"] != "file://user-schema.json" {
		t.Errorf("Expected user=file://user-schema.json, got %s", declarations["user"])
	}

	if declarations["api"] != "github.com/company/api-schema.json" {
		t.Errorf("Expected api=github.com/company/api-schema.json, got %s", declarations["api"])
	}
}

// TestLoadDeclaredSchemas tests loading schemas from declarations
func TestLoadDeclaredSchemas(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test schema files
	userSchema := `{"type": "object", "properties": {"name": {"type": "string"}}}`
	if err := os.WriteFile(filepath.Join(tmpDir, "user.json"), []byte(userSchema), 0644); err != nil {
		t.Fatalf("Failed to create test schema: %v", err)
	}

	declarations := map[string]string{
		"user": "file://user.json",
	}

	schemas, err := LoadDeclaredSchemas(declarations, tmpDir)
	if err != nil {
		t.Fatalf("Failed to load schemas: %v", err)
	}

	if len(schemas) != 1 {
		t.Errorf("Expected 1 schema, got %d", len(schemas))
	}

	if schemas["user"] == nil {
		t.Error("Expected user schema to be loaded")
	}

	if schemas["user"].Type != "object" {
		t.Errorf("Expected object type, got %v", schemas["user"].Type)
	}
}

func TestLoadFileSchema(t *testing.T) {
	// Create a temporary directory for test schemas
	tmpDir := t.TempDir()

	// Create a test schema file
	schemaContent := `{
  "type": "object",
  "properties": {
    "name": {"type": "string"}
  }
}`
	schemaPath := filepath.Join(tmpDir, "test-schema.json")
	if err := os.WriteFile(schemaPath, []byte(schemaContent), 0644); err != nil {
		t.Fatalf("Failed to create test schema file: %v", err)
	}

	// Load the schema
	schema, err := LoadFileSchema("file://test-schema.json", tmpDir)
	if err != nil {
		t.Fatalf("Failed to load schema: %v", err)
	}

	if schema == nil {
		t.Fatal("Expected schema, got nil")
	}

	if schema.Type != "object" {
		t.Errorf("Expected type=object, got %v", schema.Type)
	}

	if _, ok := schema.Properties["name"]; !ok {
		t.Error("Expected 'name' property in schema")
	}
}

func TestLoadFileSchemaRelativePath(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	schemaDir := filepath.Join(tmpDir, "schemas")
	if err := os.MkdirAll(schemaDir, 0755); err != nil {
		t.Fatalf("Failed to create schema directory: %v", err)
	}

	// Create a test schema file
	schemaContent := `{"type": "string"}`
	schemaPath := filepath.Join(schemaDir, "simple.json")
	if err := os.WriteFile(schemaPath, []byte(schemaContent), 0644); err != nil {
		t.Fatalf("Failed to create test schema file: %v", err)
	}

	// Load the schema with relative path
	schema, err := LoadFileSchema("file://schemas/simple.json", tmpDir)
	if err != nil {
		t.Fatalf("Failed to load schema with relative path: %v", err)
	}

	if schema == nil {
		t.Fatal("Expected schema, got nil")
	}

	if schema.Type != "string" {
		t.Errorf("Expected type=string, got %v", schema.Type)
	}
}
