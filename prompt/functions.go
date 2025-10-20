package prompt

import (
	"encoding/json"
	"strings"

	"github.com/agentflare-ai/go-jsonschema"
	"github.com/agentflare-ai/go-xmldom"
)

// SendFunction represents a send_* function declaration for LLMs
type SendFunction struct {
	Name        string
	Description string
	Schema      *jsonschema.Schema
}

// BuildSendFunctions builds function declarations for send events from available transitions.
// This is used by multiple LLM integrations (Gemini, Ollama, etc.) to create
// consistent function declarations for state machine event sending.
func BuildSendFunctions(transitions []xmldom.Element) []SendFunction {
	var functions []SendFunction
	seen := map[string]struct{}{}

	for _, t := range transitions {
		// Get the event attribute value as a string (not iterate over its characters)
		// Try "event" first (regular transitions), then "events" (runtime:transition elements)
		eventName := string(t.GetAttribute("event"))
		if eventName == "" {
			eventName = string(t.GetAttribute("events"))
		}
		if eventName == "" {
			continue
		}
		// runtime:transition may have multiple space-separated events, take the first one
		if strings.Contains(eventName, " ") {
			eventName = strings.Split(eventName, " ")[0]
		}
		if _, ok := seen[eventName]; ok {
			continue
		}

		// Create schema for this event
		ps := &jsonschema.Schema{
			Type:       jsonschema.TypeObject,
			Properties: map[string]*jsonschema.Schema{},
		}

		// Attach event-specific data schema if present
		if schemaAttr := string(t.GetAttribute("schema")); schemaAttr != "" {
			// Parse the JSON schema from the attribute
			var dataSchema jsonschema.Schema
			if err := json.Unmarshal([]byte(schemaAttr), &dataSchema); err == nil {
				// Use the parsed schema as the data property
				ps.Properties["data"] = &dataSchema
			} else {
				// If parsing fails, use a generic object schema as fallback
				ps.Properties["data"] = &jsonschema.Schema{Type: jsonschema.TypeObject}
			}
		}

		functions = append(functions, SendFunction{
			Name:        "send_" + strings.ReplaceAll(eventName, ".", "_"),
			Description: "Send event '" + eventName + "' through the SCXML interpreter",
			Schema:      ps,
		})
		seen[eventName] = struct{}{}
	}

	return functions
}
