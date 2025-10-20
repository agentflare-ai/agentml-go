package openai

import (
	"strings"
	"testing"

	"github.com/agentflare-ai/agentml-go/prompt"
	"github.com/agentflare-ai/go-xmldom"
)

func TestExtractTransitions(t *testing.T) {
	tests := []struct {
		name          string
		xml           string
		expectedCount int
		expectedNames []string
	}{
		{
			name: "Extract basic transitions",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="user.request" target="s2"/>
		<transition event="system.error" target="error"/>
	</state>
	<state id="s2"/>
</scxml>`,
			expectedCount: 2,
			expectedNames: []string{"user.request", "system.error"},
		},
		{
			name: "Extract runtime:send elements",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml"
       xmlns:runtime="urn:gogo:scxml:runtime:1"
       version="1.0">
	<state id="s1">
		<transition event="user.request" target="s2"/>
	</state>
	<runtime:send event="system.notification"/>
</scxml>`,
			expectedCount: 2,
			expectedNames: []string{"user.request", "system.notification"},
		},
		{
			name: "Empty document",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1"/>
</scxml>`,
			expectedCount: 0,
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := xmldom.NewDecoder(strings.NewReader(tt.xml))
			doc, err := decoder.Decode()
			if err != nil {
				t.Fatalf("Failed to parse XML: %v", err)
			}

			transitions := extractTransitions(doc)

			if len(transitions) != tt.expectedCount {
				t.Errorf("Expected %d transitions, got %d", tt.expectedCount, len(transitions))
			}

			// Verify event names
			for i, expectedName := range tt.expectedNames {
				if i >= len(transitions) {
					t.Errorf("Missing transition for event %s", expectedName)
					continue
				}
				actualName := string(transitions[i].GetAttribute("event"))
				if actualName != expectedName {
					t.Errorf("Expected event name '%s', got '%s'", expectedName, actualName)
				}
			}
		})
	}
}

func TestSanitizeFunctionName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"send_user.request", "send_user_request"},
		{"send_task.complete.success", "send_task_complete_success"},
		{"send_valid_name", "send_valid_name"},
		{"send_with-hyphen", "send_with-hyphen"},
		{"send_123", "send_123"},
		{"send_CamelCase", "send_CamelCase"},
		{"send_with spaces", "send_with_spaces"},
		{"send_with@special#chars", "send_with_special_chars"},
		{"send_", "send_"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeFunctionName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeFunctionName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConvertToOpenAIToolsWithMapping(t *testing.T) {
	// Test the integration: extract transitions -> build functions -> convert to OpenAI tools
	xml := `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="user.request" target="s2"/>
		<transition event="system.error" target="error"/>
		<transition event="task_complete" target="done"/>
	</state>
</scxml>`

	decoder := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse XML: %v", err)
	}

	// Extract transitions
	transitions := extractTransitions(doc)
	if len(transitions) != 3 {
		t.Fatalf("Expected 3 transitions, got %d", len(transitions))
	}

	// Log what we extracted
	t.Log("Extracted transitions:")
	for i, trans := range transitions {
		eventName := string(trans.GetAttribute("event"))
		t.Logf("  [%d] event='%s'", i, eventName)
	}

	// Build send functions using the prompt package
	sendFunctions := prompt.BuildSendFunctions(transitions)

	if len(sendFunctions) != 3 {
		t.Fatalf("Expected 3 send functions, got %d", len(sendFunctions))
		for i, fn := range sendFunctions {
			t.Logf("  [%d] %s", i, fn.Name)
		}
	}

	// Convert to OpenAI tools
	tools, mapping := convertToOpenAIToolsWithMapping(sendFunctions)

	if len(tools) != 3 {
		t.Fatalf("Expected 3 tools, got %d", len(tools))
	}

	// Verify tool names and mapping
	expectedMappings := map[string]string{
		"send_user_request": "user.request",
		"send_system_error": "system.error",
		"send_task_complete": "task_complete",
	}

	for sanitized, original := range expectedMappings {
		if mapping[sanitized] != original {
			t.Errorf("Expected mapping[%s] = %s, got %s", sanitized, original, mapping[sanitized])
		}
	}
}
