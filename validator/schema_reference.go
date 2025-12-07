package validator

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/agentflare-ai/go-jsonpointer"
	"github.com/agentflare-ai/go-jsonschema"
	"github.com/agentflare-ai/go-xmldom"
)

// SchemaReference represents a parsed schema reference
type SchemaReference struct {
	IsInline  bool   // true if inline JSON schema, false if namespace pointer
	Namespace string // namespace prefix (e.g., "user" from "user:/definitions/User")
	Pointer   string // JSON pointer path (e.g., "/definitions/User")
	Inline    string // inline JSON schema string if IsInline is true
}

// ParseSchemaReference parses a schema attribute value
// Returns SchemaReference with either inline JSON or namespace+pointer
func ParseSchemaReference(schemaAttr string) (*SchemaReference, error) {
	if schemaAttr == "" {
		return nil, fmt.Errorf("schema attribute cannot be empty")
	}

	// Check if it's inline JSON (starts with {)
	trimmed := strings.TrimSpace(schemaAttr)
	if strings.HasPrefix(trimmed, "{") {
		return &SchemaReference{
			IsInline: true,
			Inline:   trimmed,
		}, nil
	}

	// Parse namespace pointer format: "prefix:/path/to/schema"
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid schema reference format: %s (expected 'prefix:/path' or '{...}')", schemaAttr)
	}

	namespace := parts[0]
	pointer := parts[1]

	// Validate namespace prefix (should be identifier-like)
	if namespace == "" || !isValidNamespacePrefix(namespace) {
		return nil, fmt.Errorf("invalid namespace prefix: %s", namespace)
	}

	// Validate pointer starts with /
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("invalid JSON pointer path: %s (must start with /)", pointer)
	}

	return &SchemaReference{
		IsInline:  false,
		Namespace: namespace,
		Pointer:   pointer,
	}, nil
}

// isValidNamespacePrefix checks if a string is a valid namespace prefix
func isValidNamespacePrefix(s string) bool {
	if len(s) == 0 {
		return false
	}

	// First character must be letter
	first := rune(s[0])
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
		return false
	}

	// Remaining characters can be letters or digits
	for _, r := range s[1:] {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}

	return true
}

// ResolveSchemaPointer resolves a JSON pointer within a schema
// Returns the sub-schema at the specified path
func ResolveSchemaPointer(schema *jsonschema.Schema, pointer string) (*jsonschema.Schema, error) {
	if pointer == "" || pointer == "/" {
		return schema, nil
	}

	// Convert schema to map[string]interface{} for JSON pointer navigation
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	var schemaDoc interface{}
	if err := json.Unmarshal(schemaBytes, &schemaDoc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	// Parse and apply JSON pointer
	ptr, err := jsonpointer.New(pointer)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON pointer %s: %w", pointer, err)
	}

	value, err := ptr.Get(schemaDoc)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pointer %s: %w", pointer, err)
	}

	// Convert result back to JSON schema
	valueBytes, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resolved value: %w", err)
	}

	var resolvedSchema jsonschema.Schema
	if err := json.Unmarshal(valueBytes, &resolvedSchema); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resolved schema: %w", err)
	}

	return &resolvedSchema, nil
}

// ExtractSchemaDeclarations extracts schema:* attribute declarations from the root element.
// These attributes must be in the http://agentflare.ai/agentml/schema namespace.
// Returns a map of namespace prefix -> URI.
func ExtractSchemaDeclarations(rootElement xmldom.Element) (map[string]string, error) {
	if rootElement == nil {
		return nil, fmt.Errorf("root element cannot be nil")
	}

	const schemaNamespace = "http://agentflare.ai/agentml/schema"
	declarations := make(map[string]string)

	// Iterate over all attributes
	attrs := rootElement.Attributes()
	for i := uint(0); i < attrs.Length(); i++ {
		attr := attrs.Item(i)
		if attr == nil {
			continue
		}

		// Check if attribute is in the schema namespace
		attrNS := string(attr.NamespaceURI())
		if attrNS == schemaNamespace {
			// The local name is the schema prefix (e.g., "user", "api")
			prefix := string(attr.LocalName())
			attrValue := string(attr.NodeValue())

			if prefix == "" {
				continue // Skip invalid attribute
			}

			if !isValidNamespacePrefix(prefix) {
				return nil, fmt.Errorf("invalid schema namespace prefix: %s", prefix)
			}

			declarations[prefix] = attrValue
			slog.Debug("[SCHEMA_DECLARATIONS] Found schema declaration", "prefix", prefix, "uri", attrValue)
		}
	}

	return declarations, nil
}

// LoadDeclaredSchemas loads all schemas declared via schema:* attributes
// Returns a map of namespace prefix -> loaded schema
func LoadDeclaredSchemas(declarations map[string]string, baseDir string) (map[string]*jsonschema.Schema, error) {
	schemas := make(map[string]*jsonschema.Schema)

	for prefix, uri := range declarations {
		slog.Debug("[SCHEMA_LOADER] Loading schema", "prefix", prefix, "uri", uri)

		schema, err := LoadSchemaFromURI(uri, baseDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load schema for prefix '%s' from %s: %w", prefix, uri, err)
		}

		schemas[prefix] = schema
		slog.Debug("[SCHEMA_LOADER] Successfully loaded schema", "prefix", prefix)
	}

	return schemas, nil
}

// ResolveSchemaReference resolves a schema reference to an actual schema
// Handles both inline JSON and namespace pointers
func ResolveSchemaReference(ref *SchemaReference, loadedSchemas map[string]*jsonschema.Schema) (*jsonschema.Schema, error) {
	if ref.IsInline {
		// Parse inline JSON schema
		var schema jsonschema.Schema
		if err := json.Unmarshal([]byte(ref.Inline), &schema); err != nil {
			return nil, fmt.Errorf("failed to parse inline JSON schema: %w", err)
		}
		return &schema, nil
	}

	// Look up namespace in loaded schemas
	baseSchema, ok := loadedSchemas[ref.Namespace]
	if !ok {
		return nil, fmt.Errorf("schema namespace '%s' not found (available: %v)", ref.Namespace, mapKeys(loadedSchemas))
	}

	// Resolve JSON pointer within the schema
	return ResolveSchemaPointer(baseSchema, ref.Pointer)
}

// mapKeys returns the keys of a map as a slice
func mapKeys(m map[string]*jsonschema.Schema) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
