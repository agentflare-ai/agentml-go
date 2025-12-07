package validator

import (
	"testing"

	"github.com/agentflare-ai/go-jsonschema"
)

func TestParseSchemaReference(t *testing.T) {
	tests := []struct {
		name        string
		schemaAttr  string
		wantInline  bool
		wantNs      string
		wantPointer string
		wantErr     bool
	}{
		{
			name:       "inline JSON schema",
			schemaAttr: `{"type": "object"}`,
			wantInline: true,
			wantErr:    false,
		},
		{
			name:        "namespace pointer",
			schemaAttr:  "user:/definitions/User",
			wantInline:  false,
			wantNs:      "user",
			wantPointer: "/definitions/User",
			wantErr:     false,
		},
		{
			name:        "namespace pointer with root",
			schemaAttr:  "api:/",
			wantInline:  false,
			wantNs:      "api",
			wantPointer: "/",
			wantErr:     false,
		},
		{
			name:        "namespace pointer with complex path",
			schemaAttr:  "schemas:/definitions/models/User",
			wantInline:  false,
			wantNs:      "schemas",
			wantPointer: "/definitions/models/User",
			wantErr:     false,
		},
		{
			name:       "empty string",
			schemaAttr: "",
			wantErr:    true,
		},
		{
			name:       "invalid format - no colon",
			schemaAttr: "invalid",
			wantErr:    true,
		},
		{
			name:       "invalid format - colon but no slash",
			schemaAttr: "user:definitions",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseSchemaReference(tt.schemaAttr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSchemaReference() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if ref.IsInline != tt.wantInline {
				t.Errorf("IsInline = %v, want %v", ref.IsInline, tt.wantInline)
			}

			if !tt.wantInline {
				if ref.Namespace != tt.wantNs {
					t.Errorf("Namespace = %v, want %v", ref.Namespace, tt.wantNs)
				}
				if ref.Pointer != tt.wantPointer {
					t.Errorf("Pointer = %v, want %v", ref.Pointer, tt.wantPointer)
				}
			}
		})
	}
}

func TestResolveSchemaPointer(t *testing.T) {
	// Create a test schema with nested definitions
	rootSchema := &jsonschema.Schema{
		Type: jsonschema.TypeObject,
		Definitions: map[string]*jsonschema.Schema{
			"User": {
				Type: jsonschema.TypeObject,
				Properties: map[string]*jsonschema.Schema{
					"name": {Type: jsonschema.TypeString},
					"age":  {Type: jsonschema.TypeInteger},
				},
				Required: []string{"name"},
			},
			"Address": {
				Type: jsonschema.TypeObject,
				Properties: map[string]*jsonschema.Schema{
					"street": {Type: jsonschema.TypeString},
					"city":   {Type: jsonschema.TypeString},
				},
			},
		},
	}

	tests := []struct {
		name    string
		pointer string
		wantErr bool
		check   func(*testing.T, *jsonschema.Schema)
	}{
		{
			name:    "root pointer",
			pointer: "/",
			wantErr: false,
			check: func(t *testing.T, s *jsonschema.Schema) {
				if s.Type != jsonschema.TypeObject {
					t.Errorf("Expected object type, got %v", s.Type)
				}
			},
		},
		{
			name:    "definition pointer",
			pointer: "/definitions/User",
			wantErr: false,
			check: func(t *testing.T, s *jsonschema.Schema) {
				if s.Type != jsonschema.TypeObject {
					t.Errorf("Expected object type, got %v", s.Type)
				}
				if len(s.Required) != 1 || s.Required[0] != "name" {
					t.Errorf("Expected required=[name], got %v", s.Required)
				}
			},
		},
		{
			name:    "nested property pointer",
			pointer: "/definitions/User/properties/name",
			wantErr: false,
			check: func(t *testing.T, s *jsonschema.Schema) {
				if s.Type != jsonschema.TypeString {
					t.Errorf("Expected string type, got %v", s.Type)
				}
			},
		},
		{
			name:    "invalid pointer - non-existent definition",
			pointer: "/definitions/NonExistent",
			wantErr: true,
		},
		{
			name:    "invalid pointer - malformed",
			pointer: "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := ResolveSchemaPointer(rootSchema, tt.pointer)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveSchemaPointer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if tt.check != nil {
				tt.check(t, schema)
			}
		})
	}
}

func TestResolveSchemaReference(t *testing.T) {
	// Create test schemas
	userSchema := &jsonschema.Schema{
		Type: jsonschema.TypeObject,
		Definitions: map[string]*jsonschema.Schema{
			"User": {
				Type: jsonschema.TypeObject,
				Properties: map[string]*jsonschema.Schema{
					"id":   {Type: jsonschema.TypeInteger},
					"name": {Type: jsonschema.TypeString},
				},
			},
		},
	}

	apiSchema := &jsonschema.Schema{
		Type: jsonschema.TypeObject,
		Properties: map[string]*jsonschema.Schema{
			"status": {Type: jsonschema.TypeString},
		},
	}

	loadedSchemas := map[string]*jsonschema.Schema{
		"user": userSchema,
		"api":  apiSchema,
	}

	tests := []struct {
		name    string
		ref     *SchemaReference
		wantErr bool
		check   func(*testing.T, *jsonschema.Schema)
	}{
		{
			name: "inline schema",
			ref: &SchemaReference{
				IsInline: true,
				Inline:   `{"type": "string"}`,
			},
			wantErr: false,
			check: func(t *testing.T, s *jsonschema.Schema) {
				if s.Type != jsonschema.TypeString {
					t.Errorf("Expected string type, got %v", s.Type)
				}
			},
		},
		{
			name: "namespace pointer to root",
			ref: &SchemaReference{
				IsInline:  false,
				Namespace: "api",
				Pointer:   "/",
			},
			wantErr: false,
			check: func(t *testing.T, s *jsonschema.Schema) {
				if s.Type != jsonschema.TypeObject {
					t.Errorf("Expected object type, got %v", s.Type)
				}
			},
		},
		{
			name: "namespace pointer to definition",
			ref: &SchemaReference{
				IsInline:  false,
				Namespace: "user",
				Pointer:   "/definitions/User",
			},
			wantErr: false,
			check: func(t *testing.T, s *jsonschema.Schema) {
				if s.Type != jsonschema.TypeObject {
					t.Errorf("Expected object type, got %v", s.Type)
				}
				if _, ok := s.Properties["name"]; !ok {
					t.Errorf("Expected name property")
				}
			},
		},
		{
			name: "namespace not found",
			ref: &SchemaReference{
				IsInline:  false,
				Namespace: "nonexistent",
				Pointer:   "/",
			},
			wantErr: true,
		},
		{
			name: "invalid inline JSON",
			ref: &SchemaReference{
				IsInline: true,
				Inline:   `{invalid json`,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := ResolveSchemaReference(tt.ref, loadedSchemas)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveSchemaReference() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if tt.check != nil {
				tt.check(t, schema)
			}
		})
	}
}
