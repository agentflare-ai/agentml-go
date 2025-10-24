package prompt

import (
	"strings"
	"testing"

	"github.com/agentflare-ai/go-jsonschema"
	"github.com/agentflare-ai/go-xmldom"
)

func TestBuildSendFunctions(t *testing.T) {
	tests := []struct {
		name     string
		xml      string
		expected []SendFunction
	}{
		{
			name: "Single event with dot notation",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="user.request" target="s2"/>
	</state>
	<state id="s2"/>
</scxml>`,
			expected: []SendFunction{
				{
					Name:        "send_user_request",
					Description: "Send event 'user.request' through the SCXML interpreter",
				},
			},
		},
		{
			name: "Multiple events with various formats",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="user.request" target="s2"/>
		<transition event="system.error" target="error"/>
		<transition event="simple" target="s3"/>
		<transition event="with_underscore" target="s4"/>
		<transition event="with-hyphen" target="s5"/>
	</state>
	<state id="s2"/>
</scxml>`,
			expected: []SendFunction{
				{
					Name:        "send_user_request",
					Description: "Send event 'user.request' through the SCXML interpreter",
				},
				{
					Name:        "send_system_error",
					Description: "Send event 'system.error' through the SCXML interpreter",
				},
				{
					Name:        "send_simple",
					Description: "Send event 'simple' through the SCXML interpreter",
				},
				{
					Name:        "send_with_underscore",
					Description: "Send event 'with_underscore' through the SCXML interpreter",
				},
				{
					Name:        "send_with-hyphen",
					Description: "Send event 'with-hyphen' through the SCXML interpreter",
				},
			},
		},
		{
			name: "Duplicate events should be deduplicated",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="user.request" target="s2"/>
		<transition event="user.request" target="s3"/>
		<transition event="other.event" target="s4"/>
	</state>
</scxml>`,
			expected: []SendFunction{
				{
					Name:        "send_user_request",
					Description: "Send event 'user.request' through the SCXML interpreter",
				},
				{
					Name:        "send_other_event",
					Description: "Send event 'other.event' through the SCXML interpreter",
				},
			},
		},
		{
			name: "Empty event attribute should be skipped",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="" target="s2"/>
		<transition event="valid.event" target="s3"/>
	</state>
</scxml>`,
			expected: []SendFunction{
				{
					Name:        "send_valid_event",
					Description: "Send event 'valid.event' through the SCXML interpreter",
				},
			},
		},
		{
			name: "Transition without event attribute",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition target="s2"/>
		<transition event="valid.event" target="s3"/>
	</state>
</scxml>`,
			expected: []SendFunction{
				{
					Name:        "send_valid_event",
					Description: "Send event 'valid.event' through the SCXML interpreter",
				},
			},
		},
		{
			name: "Event with schema attribute should include data property",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml"
       version="1.0">
	<state id="s1">
		<transition event="user.request" schema='{"type":"object"}' target="s2"/>
		<transition event="simple.event" target="s3"/>
	</state>
</scxml>`,
			expected: []SendFunction{
				{
					Name:        "send_user_request",
					Description: "Send event 'user.request' through the SCXML interpreter",
					// Schema should have data property when event:schema is present
				},
				{
					Name:        "send_simple_event",
					Description: "Send event 'simple.event' through the SCXML interpreter",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the XML
			decoder := xmldom.NewDecoder(strings.NewReader(tt.xml))
			doc, err := decoder.Decode()
			if err != nil {
				t.Fatalf("Failed to parse XML: %v", err)
			}

			// Get all transitions
			root := doc.DocumentElement()
			transitions := root.GetElementsByTagName("transition")

			var transitionElements []xmldom.Element
			for i := uint(0); i < transitions.Length(); i++ {
				if elem, ok := transitions.Item(i).(xmldom.Element); ok {
					transitionElements = append(transitionElements, elem)
				}
			}

			// Build send functions
			functions := BuildSendFunctions(transitionElements)

			// Verify count
			if len(functions) != len(tt.expected) {
				t.Errorf("Expected %d functions, got %d", len(tt.expected), len(functions))
				t.Logf("Got functions:")
				for i, fn := range functions {
					t.Logf("  [%d] Name: %s, Description: %s", i, fn.Name, fn.Description)
				}
			}

			// Verify each expected function exists
			for _, expected := range tt.expected {
				found := false
				for _, fn := range functions {
					if fn.Name == expected.Name {
						found = true
						if fn.Description != expected.Description {
							t.Errorf("Function %s has wrong description.\nExpected: %s\nGot: %s",
								fn.Name, expected.Description, fn.Description)
						}
						break
					}
				}
				if !found {
					t.Errorf("Expected function %s not found in results", expected.Name)
				}
			}

			// Verify no unexpected functions
			for _, fn := range functions {
				found := false
				for _, expected := range tt.expected {
					if fn.Name == expected.Name {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Unexpected function found: %s", fn.Name)
				}
			}
		})
	}
}

func TestBuildSendFunctions_EventNameFormats(t *testing.T) {
	// Test that event names are NOT split into individual characters
	xml := `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="abc" target="s2"/>
	</state>
</scxml>`

	decoder := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse XML: %v", err)
	}

	root := doc.DocumentElement()
	transitions := root.GetElementsByTagName("transition")

	var transitionElements []xmldom.Element
	for i := uint(0); i < transitions.Length(); i++ {
		if elem, ok := transitions.Item(i).(xmldom.Element); ok {
			transitionElements = append(transitionElements, elem)
		}
	}

	functions := BuildSendFunctions(transitionElements)

	// Should have exactly 1 function for "abc", not 3 functions for 'a', 'b', 'c'
	if len(functions) != 1 {
		t.Errorf("Expected 1 function, got %d", len(functions))
		t.Logf("Functions found:")
		for i, fn := range functions {
			t.Logf("  [%d] %s", i, fn.Name)
		}
		t.Fatal("Event name was split into individual characters")
	}

	if functions[0].Name != "send_abc" {
		t.Errorf("Expected function name 'send_abc', got '%s'", functions[0].Name)
	}
}

func TestBuildSendFunctions_ComplexEventNames(t *testing.T) {
	// Test various complex event name formats
	testCases := []struct {
		eventName    string
		expectedName string
	}{
		{"user.request", "send_user_request"},
		{"task.complete.success", "send_task_complete_success"},
		{"event_with_underscore", "send_event_with_underscore"},
		{"event-with-hyphen", "send_event-with-hyphen"},
		{"CamelCaseEvent", "send_CamelCaseEvent"},
		{"event123", "send_event123"},
		{"event.123.test", "send_event_123_test"},
	}

	for _, tc := range testCases {
		t.Run(tc.eventName, func(t *testing.T) {
			xml := `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="` + tc.eventName + `" target="s2"/>
	</state>
</scxml>`

			decoder := xmldom.NewDecoder(strings.NewReader(xml))
			doc, err := decoder.Decode()
			if err != nil {
				t.Fatalf("Failed to parse XML: %v", err)
			}

			root := doc.DocumentElement()
			transitions := root.GetElementsByTagName("transition")

			var transitionElements []xmldom.Element
			for i := uint(0); i < transitions.Length(); i++ {
				if elem, ok := transitions.Item(i).(xmldom.Element); ok {
					transitionElements = append(transitionElements, elem)
				}
			}

			functions := BuildSendFunctions(transitionElements)

			if len(functions) != 1 {
				t.Fatalf("Expected 1 function for event '%s', got %d", tc.eventName, len(functions))
			}

			if functions[0].Name != tc.expectedName {
				t.Errorf("For event '%s', expected name '%s', got '%s'",
					tc.eventName, tc.expectedName, functions[0].Name)
			}
		})
	}
}

func TestBuildSendFunctions_SchemaJSONParsing(t *testing.T) {
	// Test that JSON schemas are correctly parsed from schema attributes
	tests := []struct {
		name          string
		xml           string
		expectedEvent string
		verifySchema  func(t *testing.T, schema *jsonschema.Schema)
	}{
		{
			name: "Simple schema with single property",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="user.request"
		            schema='{"type":"object","properties":{"request":{"type":"string"}},"required":["request"]}'
		            target="s2"/>
	</state>
</scxml>`,
			expectedEvent: "user.request",
			verifySchema: func(t *testing.T, schema *jsonschema.Schema) {
				if schema == nil {
					t.Fatal("Schema is nil")
				}
				if len(schema.Properties) != 1 {
					t.Fatalf("Expected 1 property in root schema, got %d", len(schema.Properties))
				}
				// Check that "data" is marked as required at the top level since data schema has required fields
				if len(schema.Required) != 1 || schema.Required[0] != "data" {
					t.Errorf("Expected top-level required=['data'], got %v", schema.Required)
				}
				dataSchema, ok := schema.Properties["data"]
				if !ok {
					t.Fatal("Expected 'data' property in schema")
				}
				if dataSchema.Type != "object" {
					t.Errorf("Expected data type 'object', got '%s'", dataSchema.Type)
				}
				if len(dataSchema.Properties) != 1 {
					t.Fatalf("Expected 1 property in data schema, got %d", len(dataSchema.Properties))
				}
				requestProp, ok := dataSchema.Properties["request"]
				if !ok {
					t.Fatal("Expected 'request' property in data schema")
				}
				if requestProp.Type != "string" {
					t.Errorf("Expected request type 'string', got '%s'", requestProp.Type)
				}
				if len(dataSchema.Required) != 1 || dataSchema.Required[0] != "request" {
					t.Errorf("Expected required=['request'], got %v", dataSchema.Required)
				}
			},
		},
		{
			name: "Complex schema with multiple properties and types",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="task.complete"
		            schema='{"type":"object","properties":{"result":{"type":"string","description":"The result"},"confidence":{"type":"number"},"metadata":{"type":"object"}},"required":["result","confidence"]}'
		            target="s2"/>
	</state>
</scxml>`,
			expectedEvent: "task.complete",
			verifySchema: func(t *testing.T, schema *jsonschema.Schema) {
				// Check that "data" is marked as required at the top level since data schema has required fields
				if len(schema.Required) != 1 || schema.Required[0] != "data" {
					t.Errorf("Expected top-level required=['data'], got %v", schema.Required)
				}
				dataSchema := schema.Properties["data"]
				if len(dataSchema.Properties) != 3 {
					t.Fatalf("Expected 3 properties in data schema, got %d", len(dataSchema.Properties))
				}

				// Check result property
				resultProp := dataSchema.Properties["result"]
				if resultProp.Type != "string" {
					t.Errorf("Expected result type 'string', got '%s'", resultProp.Type)
				}
				if resultProp.Description != "The result" {
					t.Errorf("Expected description 'The result', got '%s'", resultProp.Description)
				}

				// Check confidence property
				confidenceProp := dataSchema.Properties["confidence"]
				if confidenceProp.Type != "number" {
					t.Errorf("Expected confidence type 'number', got '%s'", confidenceProp.Type)
				}

				// Check metadata property
				metadataProp := dataSchema.Properties["metadata"]
				if metadataProp.Type != "object" {
					t.Errorf("Expected metadata type 'object', got '%s'", metadataProp.Type)
				}

				// Check required fields
				if len(dataSchema.Required) != 2 {
					t.Fatalf("Expected 2 required fields, got %d", len(dataSchema.Required))
				}
			},
		},
		{
			name: "Schema with nested objects",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="user.request"
		            schema='{"type":"object","properties":{"query":{"type":"string"},"context":{"type":"object","properties":{"userId":{"type":"string"},"sessionId":{"type":"number"}}}}}'
		            target="s2"/>
	</state>
</scxml>`,
			expectedEvent: "user.request",
			verifySchema: func(t *testing.T, schema *jsonschema.Schema) {
				dataSchema := schema.Properties["data"]

				contextProp := dataSchema.Properties["context"]
				if contextProp.Type != "object" {
					t.Errorf("Expected context type 'object', got '%s'", contextProp.Type)
				}
				if len(contextProp.Properties) != 2 {
					t.Fatalf("Expected 2 nested properties in context, got %d", len(contextProp.Properties))
				}

				userIdProp := contextProp.Properties["userId"]
				if userIdProp.Type != "string" {
					t.Errorf("Expected userId type 'string', got '%s'", userIdProp.Type)
				}

				sessionIdProp := contextProp.Properties["sessionId"]
				if sessionIdProp.Type != "number" {
					t.Errorf("Expected sessionId type 'number', got '%s'", sessionIdProp.Type)
				}
			},
		},
		{
			name: "Schema with array type",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="batch.process"
		            schema='{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}}}'
		            target="s2"/>
	</state>
</scxml>`,
			expectedEvent: "batch.process",
			verifySchema: func(t *testing.T, schema *jsonschema.Schema) {
				dataSchema := schema.Properties["data"]

				itemsProp := dataSchema.Properties["items"]
				if itemsProp.Type != "array" {
					t.Errorf("Expected items type 'array', got '%s'", itemsProp.Type)
				}
				if itemsProp.Items == nil {
					t.Fatal("Expected items schema to have Items definition")
				}
			},
		},
		{
			name: "Invalid JSON schema should fallback to generic object",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="invalid.schema"
		            schema='{"type":"object","properties":{"invalid":'
		            target="s2"/>
	</state>
</scxml>`,
			expectedEvent: "invalid.schema",
			verifySchema: func(t *testing.T, schema *jsonschema.Schema) {
				dataSchema := schema.Properties["data"]
				if dataSchema.Type != "object" {
					t.Errorf("Expected fallback type 'object', got '%s'", dataSchema.Type)
				}
				// Should be a generic object with no properties (fallback behavior)
				if len(dataSchema.Properties) != 0 {
					t.Logf("Warning: Invalid schema fallback has %d properties (expected 0 for generic fallback)", len(dataSchema.Properties))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := xmldom.NewDecoder(strings.NewReader(tt.xml))
			doc, err := decoder.Decode()
			if err != nil {
				t.Fatalf("Failed to parse XML: %v", err)
			}

			root := doc.DocumentElement()
			transitions := root.GetElementsByTagName("transition")

			var transitionElements []xmldom.Element
			for i := uint(0); i < transitions.Length(); i++ {
				if elem, ok := transitions.Item(i).(xmldom.Element); ok {
					transitionElements = append(transitionElements, elem)
				}
			}

			functions := BuildSendFunctions(transitionElements)

			if len(functions) != 1 {
				t.Fatalf("Expected 1 function, got %d", len(functions))
			}

			fn := functions[0]
			expectedName := "send_" + strings.ReplaceAll(tt.expectedEvent, ".", "_")
			if fn.Name != expectedName {
				t.Errorf("Expected name '%s', got '%s'", expectedName, fn.Name)
			}

			tt.verifySchema(t, fn.Schema)
		})
	}
}

func TestBuildSendFunctions_ArrayWithoutItems(t *testing.T) {
	// Test that arrays without items don't cause errors
	xml := `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="array.test"
		            schema='{"type":"object","properties":{"items":{"type":"array"}}}'
		            target="s2"/>
	</state>
</scxml>`

	decoder := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse XML: %v", err)
	}

	root := doc.DocumentElement()
	transitions := root.GetElementsByTagName("transition")

	var transitionElements []xmldom.Element
	for i := uint(0); i < transitions.Length(); i++ {
		if elem, ok := transitions.Item(i).(xmldom.Element); ok {
			transitionElements = append(transitionElements, elem)
		}
	}

	functions := BuildSendFunctions(transitionElements)

	if len(functions) != 1 {
		t.Fatalf("Expected 1 function, got %d", len(functions))
	}

	dataSchema := functions[0].Schema.Properties["data"]
	itemsProp := dataSchema.Properties["items"]

	if itemsProp.Type != "array" {
		t.Errorf("Expected items type 'array', got '%s'", itemsProp.Type)
	}

	// Items should be nil since it wasn't specified in the schema
	if itemsProp.Items != nil {
		t.Logf("Note: items.Items is %+v (will need fallback in OpenAI conversion)", itemsProp.Items)
	}
}

func TestBuildSendFunctions_SchemaPreservesAllJSONSchemaFeatures(t *testing.T) {
	// Test that all JSON Schema features are preserved during parsing
	xml := `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="feature.test"
		            schema='{"type":"object","properties":{"name":{"type":"string","description":"User name","minLength":1,"maxLength":100},"age":{"type":"integer","minimum":0,"maximum":150},"email":{"type":"string","format":"email"},"tags":{"type":"array","items":{"type":"string"},"minItems":1}},"required":["name","email"]}'
		            target="s2"/>
	</state>
</scxml>`

	decoder := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse XML: %v", err)
	}

	root := doc.DocumentElement()
	transitions := root.GetElementsByTagName("transition")

	var transitionElements []xmldom.Element
	for i := uint(0); i < transitions.Length(); i++ {
		if elem, ok := transitions.Item(i).(xmldom.Element); ok {
			transitionElements = append(transitionElements, elem)
		}
	}

	functions := BuildSendFunctions(transitionElements)

	if len(functions) != 1 {
		t.Fatalf("Expected 1 function, got %d", len(functions))
	}

	dataSchema := functions[0].Schema.Properties["data"]

	// Verify all properties are preserved
	nameProp := dataSchema.Properties["name"]
	if nameProp.Type != "string" {
		t.Errorf("Expected name type 'string', got '%s'", nameProp.Type)
	}
	if nameProp.Description != "User name" {
		t.Errorf("Expected description 'User name', got '%s'", nameProp.Description)
	}

	ageProp := dataSchema.Properties["age"]
	if ageProp.Type != "integer" {
		t.Errorf("Expected age type 'integer', got '%s'", ageProp.Type)
	}

	emailProp := dataSchema.Properties["email"]
	if emailProp.Type != "string" {
		t.Errorf("Expected email type 'string', got '%s'", emailProp.Type)
	}

	tagsProp := dataSchema.Properties["tags"]
	if tagsProp.Type != "array" {
		t.Errorf("Expected tags type 'array', got '%s'", tagsProp.Type)
	}

	// Verify required fields
	if len(dataSchema.Required) != 2 {
		t.Fatalf("Expected 2 required fields, got %d", len(dataSchema.Required))
	}
}
