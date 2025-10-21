package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"text/template"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/agentml-go/prompt"
	"github.com/agentflare-ai/go-jsonschema"
	"github.com/agentflare-ai/go-xmldom"
	"github.com/openai/openai-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// OpenAINamespaceURI is the XML namespace URI used for OpenAI executable elements.
const OpenAINamespaceURI = "github.com/agentflare-ai/agentml-go/openai"

// Generate represents an OpenAI generation executable content element for SCXML.
// It implements the scxml.Executable interface to provide AI generation capabilities
// within SCXML state machines using OpenAI-compatible models.
//
// The Generate struct maps to XML elements with the following attributes:
//   - model: Specifies the OpenAI model to use (e.g., "gpt-4", "gpt-4o")
//   - prompt: The prompt for AI generation
//   - location: Data model location where the generated result should be stored
//   - stream: Whether to use streaming generation (optional, default false)
//   - modelexpr: Dynamic model expression (optional)
//   - promptexpr: Dynamic prompt expression (optional)
type Generate struct {
	xmldom.Element

	// Model specifies the OpenAI model to use for generation.
	// Common values include "gpt-4", "gpt-4o", "gpt-3.5-turbo", etc.
	Model string `xml:"model,attr"`

	// Prompt contains the prompt for AI generation.
	// This can be a static string or contain data model expressions.
	Prompt string `xml:"prompt,attr"`

	// Location specifies where in the data model to store the generated result.
	// This should be a valid data model location expression.
	Location string `xml:"location,attr"`

	// Stream indicates whether to use streaming generation for real-time responses.
	// When true, responses are delivered progressively as they are generated.
	Stream bool `xml:"stream,attr"`

	// client is the OpenAI client for making API calls
	client *Client
}

// Execute implements the scxml.Executable interface for Generate.
// It performs AI generation using the specified OpenAI model and prompt,
// then stores the result in the specified data model location.
//
// The execution process:
//  1. Validates that all required attributes are present
//  2. Evaluates the prompt expression using the data model (if needed)
//  3. Calls the OpenAI API to generate content
//  4. Stores the generated result in the specified location
//
// Returns an error if generation fails or if required attributes are missing.
func (g *Generate) Execute(ctx context.Context, interpreter agentml.Interpreter) error {
	// Validate required attributes
	modelExpr := string(g.Element.GetAttribute("modelexpr"))
	if g.Model == "" && modelExpr == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "Generate element missing required 'model' or 'modelexpr' attribute",
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     fmt.Errorf("generate element missing required 'model' or 'modelexpr' attribute"),
		}
	}

	// Location is only required when no tools will be available (fallback case)
	// When tools are available, the LLM drives state transitions through function calls
	if g.Location == "" {
		// Check if we expect to have tools - if so, location is optional
		// We'll validate this after building the system prompt and tools
		slog.Debug("[OPENAI] Location not specified - will validate after tool detection")
	}

	dataModel := interpreter.DataModel()
	if dataModel == nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "No data model available for OpenAI generation",
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     fmt.Errorf("no data model available for openai generation"),
		}
	}

	// Support dynamic modelexpr (evaluated via data model)
	modelName := g.Model
	if me := string(g.Element.GetAttribute("modelexpr")); strings.TrimSpace(me) != "" {
		v, err := dataModel.EvaluateValue(ctx, me)
		if err == nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				modelName = s
			}
		}
	}

	// Start OpenTelemetry span for execution tracking
	tracer := otel.Tracer("openai")
	ctx, span := tracer.Start(ctx, "openai.generate.execute",
		trace.WithAttributes(
			attribute.String("openai.model", modelName),
			attribute.String("openai.location", g.Location),
		),
	)
	defer span.End()

	// Evaluate prompt expression if it contains data model expressions
	promptText, err := g.evaluatePrompt(ctx, interpreter)
	if err != nil {
		span.RecordError(err)
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("Failed to evaluate prompt expression: %v", err),
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     err,
		}
	}
	slog.Debug("[OPENAI] Prompt text", "text", promptText)

	// Also support dynamic promptexpr (evaluated via data model)
	if pe := string(g.Element.GetAttribute("promptexpr")); strings.TrimSpace(pe) != "" {
		v, err := dataModel.EvaluateValue(ctx, pe)
		if err == nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				if promptText != "" {
					promptText += "\n"
				}
				promptText += s
			}
		}
	}

	// Process child <openai:prompt> elements as templates
	childPrompts, err := g.processChildPrompts(ctx, interpreter)
	if err != nil {
		span.RecordError(err)
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("Failed to process child prompt elements: %v", err),
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     err,
		}
	}

	// Combine prompts - attribute prompt first, then child prompts
	finalPrompt := promptText
	if len(childPrompts) > 0 {
		if finalPrompt != "" {
			finalPrompt += "\n" + strings.Join(childPrompts, "\n")
		} else {
			finalPrompt = strings.Join(childPrompts, "\n")
		}
	}

	// Build system instruction from SCXML snapshot and extract available transitions
	var systemPrompt string
	var openaiTools []openai.ChatCompletionToolParam
	var eventNameMapping map[string]string // Maps sanitized function names to original event names
	if doc, err := interpreter.Snapshot(ctx, agentml.SnapshotConfig{}); err == nil {
		// Extract transitions from snapshot to build dynamic tools BEFORE pruning
		// (pruning removes runtime:actions which contains the scoped transitions)
		transitions := extractTransitions(doc)
		sendFunctions := prompt.BuildSendFunctions(transitions)
		openaiTools, eventNameMapping = convertToOpenAIToolsWithMapping(sendFunctions)
		slog.Debug("openai.generate.execute: built tools from snapshot", "count", len(openaiTools), "mapping", eventNameMapping)

		// Prune redundant information from snapshot
		prompt.PruneSnapshot(doc)

		slog.Debug("openai.generate.execute: pruned snapshot ready")

		// Marshal and compress for minimal token usage
		if b, err2 := xmldom.MarshalIndentWithOptions(doc, "", "  ", true, false); err2 == nil {
			systemPrompt = prompt.CompressXML(string(b))
		}
	}

	span.SetAttributes(attribute.String("openai.prompt_length", fmt.Sprintf("%d", len(finalPrompt))))

	// Get or initialize OpenAI client
	client := g.client
	if client == nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "OpenAI client not configured. Use SetClient() to configure the client.",
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     fmt.Errorf("openai client not configured"),
		}
	}

	// Build messages with system prompt and user prompt
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
		openai.UserMessage(finalPrompt),
	}

	// Validate location requirement based on tool availability
	if len(openaiTools) == 0 && g.Location == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "Generate element missing required 'location' attribute (no tools available for function calls)",
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     fmt.Errorf("location required when no tools are available"),
		}
	}

	// Use ChatWithTools if tools are available
	var response *openai.ChatCompletion
	if len(openaiTools) > 0 {
		response, err = client.ChatWithTools(ctx, ModelName(modelName), messages, openaiTools, g.Stream)
		if err != nil {
			span.RecordError(err)
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to generate content: %v", err),
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     err,
			}
		}

		// Process tool calls
		if err := processOpenAIToolCalls(ctx, interpreter, response, eventNameMapping); err != nil {
			span.RecordError(err)
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to process tool calls: %v", err),
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     err,
			}
		}
	} else {
		// Fall back to regular chat without tools
		response, err = client.Chat(ctx, ModelName(modelName), messages, g.Stream)
		if err != nil {
			span.RecordError(err)
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to generate content: %v", err),
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     err,
			}
		}

		// Extract text from response and store in data model
		if len(response.Choices) == 0 {
			span.RecordError(fmt.Errorf("no choices in response"))
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   "No choices in OpenAI response",
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     fmt.Errorf("no choices in openai response"),
			}
		}

		content := response.Choices[0].Message.Content
		if err := dataModel.Assign(ctx, g.Location, content); err != nil {
			span.RecordError(err)
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to assign result to location '%s': %v", g.Location, err),
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     err,
			}
		}
	}

	return nil
}

// extractTransitions extracts only the available (scoped) transition elements from a snapshot document.
// This reads from runtime:actions/runtime:send which contains only transitions available from the current state.
func extractTransitions(doc xmldom.Document) []xmldom.Element {
	if doc == nil {
		return nil
	}

	root := doc.DocumentElement()
	if root == nil {
		return nil
	}

	var transitions []xmldom.Element

	// Look for runtime:snapshot/runtime:actions/runtime:send/runtime:transition elements
	// These are already scoped to the current state configuration
	runtimeTransitions := root.GetElementsByTagNameNS(agentml.RuntimeNamespaceURI, "transition")
	for i := uint(0); i < runtimeTransitions.Length(); i++ {
		if elem, ok := runtimeTransitions.Item(i).(xmldom.Element); ok {
			// Only include transitions that are under runtime:send (external events)
			// Walk up the parent chain to verify this is under runtime:send
			parent := elem.ParentNode()
			if parent != nil {
				if parentElem, ok := parent.(xmldom.Element); ok {
					if string(parentElem.LocalName()) == "send" &&
						parentElem.NamespaceURI() == xmldom.DOMString(agentml.RuntimeNamespaceURI) {
						transitions = append(transitions, elem)
					}
				}
			}
		}
	}

	return transitions
}

// sanitizeFunctionName sanitizes a function name to meet OpenAI's requirements.
// OpenAI requires function names to match the pattern ^[a-zA-Z0-9_-]+$
// This replaces dots and other invalid characters with underscores.
func sanitizeFunctionName(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		// Allow alphanumeric, underscore, and hyphen
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			result = append(result, c)
		} else {
			// Replace invalid characters with underscore
			result = append(result, '_')
		}
	}
	return string(result)
}

// schemaToMap converts a jsonschema.Schema to map[string]any for OpenAI API compatibility
func schemaToMap(schema *jsonschema.Schema) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object"}
	}

	result := map[string]any{}

	// Add type (convert jsonschema.JSONType to string)
	typeStr := string(schema.Type)
	if typeStr != "" {
		result["type"] = typeStr
	}

	// Add description
	if schema.Description != "" {
		result["description"] = schema.Description
	}

	// Add properties recursively
	if len(schema.Properties) > 0 {
		props := make(map[string]any)
		for key, propSchema := range schema.Properties {
			props[key] = schemaToMap(propSchema)
		}
		result["properties"] = props
	}

	// Add required fields
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	// Add items for arrays (OpenAI requires this for array types)
	if typeStr == "array" {
		if schema.Items != nil {
			result["items"] = schemaToMap(schema.Items)
		} else {
			// OpenAI requires items for array types - provide a generic object fallback
			result["items"] = map[string]any{"type": "object"}
		}
	}

	// Add enum values
	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}

	// Add format
	if schema.Format != "" {
		result["format"] = schema.Format
	}

	// Add validation constraints
	if schema.MinLength != nil && *schema.MinLength > 0 {
		result["minLength"] = *schema.MinLength
	}
	if schema.MaxLength != nil && *schema.MaxLength > 0 {
		result["maxLength"] = *schema.MaxLength
	}
	if schema.Minimum != nil {
		result["minimum"] = schema.Minimum
	}
	if schema.Maximum != nil {
		result["maximum"] = schema.Maximum
	}
	if schema.MinItems != nil && *schema.MinItems > 0 {
		result["minItems"] = *schema.MinItems
	}
	if schema.MaxItems != nil && *schema.MaxItems > 0 {
		result["maxItems"] = *schema.MaxItems
	}

	return result
}

// convertToOpenAIToolsWithMapping converts prompt.SendFunction declarations to OpenAI tool parameters
// and returns a mapping from sanitized function names to original event names.
func convertToOpenAIToolsWithMapping(sendFunctions []prompt.SendFunction) ([]openai.ChatCompletionToolParam, map[string]string) {
	var tools []openai.ChatCompletionToolParam
	mapping := make(map[string]string)

	for _, fn := range sendFunctions {
		// Sanitize function name for OpenAI (dots are not allowed)
		sanitizedName := sanitizeFunctionName(fn.Name)

		// Extract original event name (remove "send_" prefix and convert underscores to dots)
		originalEventName := strings.TrimPrefix(fn.Name, "send_")
		originalEventName = strings.ReplaceAll(originalEventName, "_", ".")
		mapping[sanitizedName] = originalEventName

		// Build parameter schema for OpenAI
		parameters := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}

		// Add target and delay as optional parameters for all send events
		if props, ok := parameters["properties"].(map[string]any); ok {
			props["target"] = map[string]any{
				"type":        "string",
				"description": "Target destination for the event (optional)",
			}
			props["delay"] = map[string]any{
				"type":        "string",
				"description": "Delay before sending the event in CSS2 format (optional)",
			}

			// Add data property if the schema has properties
			if fn.Schema != nil && len(fn.Schema.Properties) > 0 {
				// Use the actual parsed schema from the data property
				if dataSchema, hasData := fn.Schema.Properties["data"]; hasData {
					// Convert the jsonschema.Schema to map[string]any for OpenAI
					props["data"] = schemaToMap(dataSchema)
				} else {
					// Fallback to generic object if no data property
					props["data"] = map[string]any{
						"type":        "object",
						"description": "Event-specific data payload",
					}
				}
			}
		}

		// Create the function tool
		tool := openai.ChatCompletionToolParam{
			Type: openai.F(openai.ChatCompletionToolTypeFunction),
			Function: openai.F(openai.FunctionDefinitionParam{
				Name:        openai.String(sanitizedName),
				Description: openai.String(fn.Description),
				Parameters:  openai.F(openai.FunctionParameters(parameters)),
			}),
		}

		tools = append(tools, tool)
	}

	return tools, mapping
}

// processOpenAIToolCalls processes tool calls from OpenAI response and sends corresponding events.
// The eventNameMapping maps sanitized function names back to original event names.
func processOpenAIToolCalls(ctx context.Context, it agentml.Interpreter, resp *openai.ChatCompletion, eventNameMapping map[string]string) error {
	slog.Info("processOpenAIToolCalls: starting")
	if resp == nil {
		return fmt.Errorf("nil response")
	}

	if len(resp.Choices) == 0 {
		return fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]
	if len(choice.Message.ToolCalls) == 0 {
		return fmt.Errorf("no tool calls in response")
	}

	slog.Info("processOpenAIToolCalls: processing tool calls", "count", len(choice.Message.ToolCalls))
	for i, toolCall := range choice.Message.ToolCalls {
		slog.Info("processOpenAIToolCalls: processing tool call", "index", i, "function", toolCall.Function.Name)
		if toolCall.Type != openai.ChatCompletionMessageToolCallTypeFunction {
			continue
		}

		sanitizedName := toolCall.Function.Name
		if !strings.HasPrefix(sanitizedName, "send_") {
			return fmt.Errorf("unsupported function: %s (only send_* allowed)", sanitizedName)
		}

		// Map sanitized function name back to original event name
		var evName string
		slog.Info("processOpenAIToolCalls: looking up event", "sanitizedName", sanitizedName, "mappingExists", eventNameMapping != nil)
		if eventNameMapping != nil {
			if originalName, ok := eventNameMapping[sanitizedName]; ok {
				evName = originalName
				slog.Info("processOpenAIToolCalls: found in mapping", "sanitized", sanitizedName, "original", evName)
			} else {
				slog.Info("processOpenAIToolCalls: not found in mapping, using fallback", "sanitizedName", sanitizedName, "mappingKeys", eventNameMapping)
				// Fallback: extract from sanitized name and convert underscores to dots
				evName = strings.TrimPrefix(sanitizedName, "send_")
				evName = strings.ReplaceAll(evName, "_", ".")
				slog.Debug("openai.processToolCalls: no mapping found, using fallback", "sanitized", sanitizedName, "extracted", evName)
			}
		} else {
			// No mapping provided, use sanitized name directly and convert underscores to dots
			evName = strings.TrimPrefix(sanitizedName, "send_")
			evName = strings.ReplaceAll(evName, "_", ".")
			slog.Debug("openai.processToolCalls: no mapping provided, using sanitized name", "sanitized", sanitizedName, "extracted", evName)
		}

		// Parse arguments
		var arguments map[string]any
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			return fmt.Errorf("failed to parse tool call arguments: %w", err)
		}

		slog.Info("processOpenAIToolCalls: sending event to interpreter", "event", evName)
		// Use the handleSendCall helper for consistency
		if err := handleSendCall(ctx, it, evName, arguments); err != nil {
			slog.Error("processOpenAIToolCalls: handleSendCall failed", "error", err)
			return err
		}
		slog.Info("processOpenAIToolCalls: event sent successfully", "event", evName)
	}

	slog.Info("processOpenAIToolCalls: all tool calls processed")
	return nil
}

// SetClient sets the OpenAI client for this Generate instance.
// This enables dependency injection for testing and configuration.
func (g *Generate) SetClient(client *Client) {
	g.client = client
}

// evaluatePrompt evaluates the prompt attribute using the data model if it contains expressions.
// Returns the evaluated prompt text or the original text if no expressions are present.
func (g *Generate) evaluatePrompt(ctx context.Context, interpreter agentml.Interpreter) (string, error) {
	if g.Prompt == "" {
		return "", nil
	}

	// Check if prompt contains data model expressions (simple heuristic)
	if !strings.Contains(g.Prompt, "${") && !strings.Contains(g.Prompt, "{{") {
		// No expressions, return as-is
		return g.Prompt, nil
	}

	// Use data model to evaluate expressions
	dataModel := interpreter.DataModel()
	if dataModel == nil {
		return g.Prompt, nil // Fallback to literal prompt
	}

	// Try to evaluate as an expression
	result, err := dataModel.EvaluateValue(ctx, g.Prompt)
	if err != nil {
		// If evaluation fails, return the original prompt as fallback
		return g.Prompt, nil
	}

	// Convert result to string
	if str, ok := result.(string); ok {
		return str, nil
	}

	// Convert other types to string representation
	return fmt.Sprintf("%v", result), nil
}

// processChildPrompts processes child <openai:prompt> elements as Go templates.
// Returns a slice of processed prompt strings.
func (g *Generate) processChildPrompts(ctx context.Context, interpreter agentml.Interpreter) ([]string, error) {
	var prompts []string

	// Get child elements
	children := g.ChildNodes()
	if children.Length() == 0 {
		return prompts, nil
	}

	// Get data model context for template processing
	dataModel := interpreter.DataModel()
	var templateData map[string]any
	if dataModel != nil {
		// Try to get all data model variables as template context
		// This is a simplified approach - in practice you might want to expose
		// specific methods to get the current data model state
		templateData = make(map[string]any)
	}

	for i := uint(0); i < children.Length(); i++ {
		child := children.Item(i)
		element, ok := child.(xmldom.Element)
		if !ok {
			continue
		}

		// Check if this is an openai:prompt element
		localName := element.LocalName()
		namespaceURI := element.NamespaceURI()

		if string(localName) == "prompt" && (string(namespaceURI) == OpenAINamespaceURI || string(namespaceURI) == "") {
			// Get the text content of the prompt element
			promptContent := string(element.TextContent())
			promptContent = strings.TrimSpace(promptContent)
			if promptContent == "" {
				continue
			}

			// Evaluate through data model if it looks like an ECMAScript expression
			// (wrapped in backticks or contains ${...})
			if dataModel != nil && (strings.HasPrefix(promptContent, "`") || strings.Contains(promptContent, "${")) {
				result, err := dataModel.EvaluateValue(ctx, promptContent)
				if err != nil {
					// If evaluation fails, use original content
					slog.Warn("failed to evaluate prompt through data model", "error", err)
				} else if str, ok := result.(string); ok {
					promptContent = str
				}
			}

			// Process Go templates ({{...}}) for things like fetch
			processedPrompt, err := g.processTemplate(promptContent, templateData)
			if err != nil {
				return nil, fmt.Errorf("failed to process template in prompt element: %w", err)
			}

			prompts = append(prompts, processedPrompt)
		}
	}

	return prompts, nil
}

// processTemplate processes a text string as a Go template with the given data.
// Includes a custom 'fetch' function for retrieving content from URLs.
func (g *Generate) processTemplate(templateText string, data map[string]any) (string, error) {
	// If no template syntax detected, return as-is
	if !strings.Contains(templateText, "{{") {
		return templateText, nil
	}

	// Create template with custom functions
	funcMap := template.FuncMap{
		"fetch": func(url string) string {
			resp, err := http.Get(url)
			if err != nil {
				slog.Warn("fetch function failed", "url", url, "error", err)
				return ""
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				slog.Warn("fetch function received non-200 status", "url", url, "status", resp.Status)
				return ""
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				slog.Warn("fetch function failed to read response", "url", url, "error", err)
				return ""
			}

			return string(body)
		},
	}

	tmpl, err := template.New("prompt").Funcs(funcMap).Parse(templateText)
	if err != nil {
		// If template parsing fails, return original text
		return templateText, nil
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		// If template execution fails, return original text
		return templateText, nil
	}

	return buf.String(), nil
}

// NewGenerate creates a new Generate executable from an XML element.
// It constructs executable content from the provided xmldom.Element by
// extracting attributes needed for generation.
//
// The function validates that required attributes are present and returns
// an error if the element is malformed or missing required data.
//
// Parameters:
//   - ctx: Context for the operation (currently unused but follows interface)
//   - element: The XML element containing the generation configuration
//
// Returns:
//   - agentml.Executor: A new Generate instance implementing the interface
//   - error: An error if the element is invalid or missing required attributes
func NewGenerate(ctx context.Context, element xmldom.Element) (agentml.Executor, error) {
	// Validate element is not nil
	if element == nil {
		return nil, fmt.Errorf("generate element cannot be nil")
	}

	model := string(element.GetAttribute("model"))
	prompt := string(element.GetAttribute("prompt"))
	location := string(element.GetAttribute("location"))
	stream := string(element.GetAttribute("stream")) == "true"

	// Validate required attributes
	if model == "" {
		return nil, fmt.Errorf("generate element missing required 'model' attribute")
	}

	// Location is now optional (validated at runtime based on tool availability)

	// Note: prompt can be empty if content will come from child elements

	return &Generate{
		Element:  element,
		Model:    model,
		Prompt:   prompt,
		Location: location,
		Stream:   stream,
	}, nil
}

// Ensure Generate implements the agentml.Executor interface
var _ agentml.Executor = (*Generate)(nil)

// toolConfigFrom creates an OpenAI tool config from function declarations.
// This limits allowed function names to the provided declarations.
func toolConfigFrom(fns []openai.ChatCompletionToolParam) []openai.ChatCompletionToolParam {
	return fns
}

// processFunctionCalls processes function calls from OpenAI response and handles different call types.
// It supports send_*, Raise, and Cancel function calls, and optionally allows text responses.
func processFunctionCalls(ctx context.Context, it agentml.Interpreter, resp *openai.ChatCompletion, fns []openai.ChatCompletionToolParam, allowText bool, location string) error {
	if resp == nil {
		return fmt.Errorf("nil response")
	}
	dm := it.DataModel()
	processed := false
	for _, choice := range resp.Choices {
		if len(choice.Message.ToolCalls) == 0 {
			// Handle text response if allowed
			if choice.Message.Content != "" && allowText {
				if location != "" && dm != nil {
					_ = dm.Assign(ctx, location, choice.Message.Content)
				}
				continue
			}
			if choice.Message.Content != "" {
				return fmt.Errorf("non-function content is not allowed; model must return function calls only")
			}
			continue
		}

		for _, toolCall := range choice.Message.ToolCalls {
			if toolCall.Type != openai.ChatCompletionMessageToolCallTypeFunction {
				continue
			}
			processed = true
			name := toolCall.Function.Name

			// Parse function arguments
			var args map[string]any
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return fmt.Errorf("failed to parse tool call arguments: %w", err)
			}

			// Handle different function types
			if strings.HasPrefix(name, "send_") {
				evName := strings.TrimPrefix(name, "send_")
				return handleSendCall(ctx, it, evName, args)
			}
			switch name {
			case "Raise":
				return handleRaiseCall(ctx, it, args)
			case "Cancel":
				return handleCancelCall(ctx, it, args)
			default:
				return fmt.Errorf("unsupported function: %s", name)
			}
		}
	}
	if !processed {
		return fmt.Errorf("no function calls in response")
	}
	return nil
}

// handleRaiseCall processes a Raise function call and raises an internal event.
func handleRaiseCall(ctx context.Context, it agentml.Interpreter, args map[string]any) error {
	name, _ := args["name"].(string)
	data := args["data"]
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("Raise.name required")
	}
	ev := &agentml.Event{
		Name: name,
		Type: agentml.EventTypeInternal,
		Data: data,
	}
	it.Raise(ctx, ev)
	return nil
}

// normalizeDelay converts various delay formats and normalizes zero delays to empty string
// Returns empty string for zero delays (immediate send) or the normalized delay string
func normalizeDelay(delay string) string {
	delay = strings.TrimSpace(delay)

	// Handle empty or zero delays - these should be immediate sends
	if delay == "" || delay == "0" || delay == "0s" || delay == "0ms" {
		return "" // Empty string means immediate send
	}

	// Handle ISO 8601 zero duration cases
	if delay == "PT0S" || delay == "P0D" {
		return "" // Empty string means immediate send
	}

	// For ISO 8601 durations, try to convert them
	if strings.HasPrefix(delay, "P") {
		// This would be the place to add full ISO 8601 to CSS2 conversion
		// For now, just handle the zero cases above
		return delay // Keep as-is if we can't convert
	}

	return delay // Return normalized delay
}

// handleSendCall processes a send_* function call and sends an external event.
func handleSendCall(ctx context.Context, it agentml.Interpreter, eventName string, args map[string]any) error {
	// Build event data from arguments
	var data any
	if d, ok := args["data"]; ok {
		data = d
	} else if len(args) > 0 {
		// Filter out target and delay if present
		filtered := make(map[string]any)
		for k, v := range args {
			if k != "target" && k != "delay" {
				filtered[k] = v
			}
		}
		if len(filtered) > 0 {
			data = filtered
		}
	}

	target, _ := args["target"].(string)
	delay, _ := args["delay"].(string)
	ev := &agentml.Event{
		Name: eventName,
		Type: agentml.EventTypeExternal,
		Data: data,
	}
	if target != "" {
		ev.Origin = target
	}
	if delay != "" {
		// Normalize delay format and convert zero delays to empty string (immediate send)
		normalizedDelay := normalizeDelay(delay)
		if normalizedDelay != delay {
			slog.WarnContext(ctx, "Normalized delay format",
				"original", delay, "normalized", normalizedDelay)
		}
		if normalizedDelay != "" {
			ev.Delay = normalizedDelay
		}
		// If normalizedDelay is empty, ev.Delay remains unset (immediate send)
	}
	return it.Send(ctx, ev)
}

// handleCancelCall processes a Cancel function call and cancels a delayed send operation.
func handleCancelCall(ctx context.Context, it agentml.Interpreter, args map[string]any) error {
	sendId, _ := args["sendId"].(string)
	if strings.TrimSpace(sendId) == "" {
		return fmt.Errorf("Cancel.sendId required")
	}
	return it.Cancel(ctx, sendId)
}
