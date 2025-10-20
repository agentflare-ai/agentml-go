package openai

import (
	"os"
	"strings"
	"testing"

	"github.com/agentflare-ai/agentml-go/prompt"
	"github.com/agentflare-ai/go-xmldom"
)

func TestDynamicToolBuilding_Integration(t *testing.T) {
	// Load the test SCXML file
	data, err := os.ReadFile("testdata/test_agent.scxml")
	if err != nil {
		t.Fatalf("Failed to read test SCXML file: %v", err)
	}

	// Parse the SCXML document
	decoder := xmldom.NewDecoder(strings.NewReader(string(data)))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse SCXML: %v", err)
	}

	t.Log("=== Testing Dynamic Tool Building from SCXML ===")

	// Step 1: Extract transitions
	transitions := extractTransitions(doc)
	t.Logf("Step 1: Extracted %d transitions", len(transitions))

	expectedTransitionCount := 6 // user.request, system.shutdown, task.complete, task.failed, retry.request, cancel.request
	if len(transitions) != expectedTransitionCount {
		t.Errorf("Expected %d transitions, got %d", expectedTransitionCount, len(transitions))
	}

	// Log all extracted transitions
	t.Log("\nExtracted transitions:")
	for i, trans := range transitions {
		eventName := string(trans.GetAttribute("event"))
		schema := string(trans.GetAttribute("schema"))
		hasSchema := schema != ""
		t.Logf("  [%d] event='%s' hasSchema=%v", i, eventName, hasSchema)
	}

	// Step 2: Build send functions
	sendFunctions := prompt.BuildSendFunctions(transitions)
	t.Logf("\nStep 2: Built %d send functions", len(sendFunctions))

	if len(sendFunctions) != expectedTransitionCount {
		t.Errorf("Expected %d send functions, got %d", expectedTransitionCount, len(sendFunctions))
		for i, fn := range sendFunctions {
			t.Logf("  [%d] %s", i, fn.Name)
		}
		t.Fatal("Wrong number of send functions")
	}

	// Verify expected functions
	expectedFunctions := map[string]bool{
		"send_user.request":   false,
		"send_system.shutdown": false,
		"send_task.complete":  false,
		"send_task.failed":    false,
		"send_retry.request":  false,
		"send_cancel.request": false,
	}

	t.Log("\nSend functions:")
	for i, fn := range sendFunctions {
		hasData := fn.Schema != nil && len(fn.Schema.Properties) > 0
		t.Logf("  [%d] %s (hasDataSchema=%v)", i, fn.Name, hasData)

		// Mark as found
		if _, ok := expectedFunctions[fn.Name]; ok {
			expectedFunctions[fn.Name] = true
		} else {
			t.Errorf("Unexpected function: %s", fn.Name)
		}
	}

	// Check all expected functions were found
	for name, found := range expectedFunctions {
		if !found {
			t.Errorf("Expected function '%s' was not found", name)
		}
	}

	// Step 3: Convert to OpenAI tools with mapping
	tools, mapping := convertToOpenAIToolsWithMapping(sendFunctions)
	t.Logf("\nStep 3: Converted to %d OpenAI tools with mapping", len(tools))

	if len(tools) != expectedTransitionCount {
		t.Errorf("Expected %d tools, got %d", expectedTransitionCount, len(tools))
	}

	// Verify sanitization and mapping
	expectedMappings := map[string]string{
		"send_user_request":    "user.request",
		"send_system_shutdown": "system.shutdown",
		"send_task_complete":   "task.complete",
		"send_task_failed":     "task.failed",
		"send_retry_request":   "retry.request",
		"send_cancel_request":  "cancel.request",
	}

	t.Log("\nOpenAI Tools and Mappings:")
	for i, tool := range tools {
		funcName := tool.Function.Value.Name.Value
		originalEvent := mapping[funcName]
		expectedOriginal, ok := expectedMappings[funcName]

		t.Logf("  [%d] %s -> %s", i, funcName, originalEvent)

		if !ok {
			t.Errorf("Unexpected tool function name: %s", funcName)
			continue
		}

		if originalEvent != expectedOriginal {
			t.Errorf("Mapping for '%s' incorrect: expected '%s', got '%s'",
				funcName, expectedOriginal, originalEvent)
		}
	}

	// Verify all expected mappings exist
	for sanitized, original := range expectedMappings {
		if mapping[sanitized] != original {
			t.Errorf("Missing or incorrect mapping: %s should map to %s, got %s",
				sanitized, original, mapping[sanitized])
		}
	}

	t.Log("\n=== Integration Test Complete ===")
}

func TestDynamicToolBuilding_StateTransitions(t *testing.T) {
	// Test with different state configurations to ensure tools change dynamically

	tests := []struct {
		name          string
		xml           string
		expectedTools []string
	}{
		{
			name: "Initial state - only user.request available",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0" initial="idle">
	<state id="idle">
		<transition event="user.request" target="processing"/>
	</state>
	<state id="processing"/>
</scxml>`,
			expectedTools: []string{"send_user_request"},
		},
		{
			name: "Processing state - multiple events available",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0" initial="processing">
	<state id="processing">
		<transition event="task.complete" target="idle"/>
		<transition event="task.failed" target="error"/>
		<transition event="cancel.request" target="idle"/>
	</state>
	<state id="idle"/>
	<state id="error"/>
</scxml>`,
			expectedTools: []string{"send_task_complete", "send_task_failed", "send_cancel_request"},
		},
		{
			name: "Complex state - events with various formats",
			xml: `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml"
       version="1.0" initial="state1">
	<state id="state1">
		<transition event="event.with.dots" target="state2" schema='{"type":"object"}'/>
		<transition event="event_with_underscores" target="state2"/>
		<transition event="event-with-hyphens" target="state2"/>
		<transition event="SimpleEvent" target="state2"/>
	</state>
	<state id="state2"/>
</scxml>`,
			expectedTools: []string{
				"send_event_with_dots",
				"send_event_with_underscores",
				"send_event-with-hyphens",
				"send_SimpleEvent",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse XML
			decoder := xmldom.NewDecoder(strings.NewReader(tt.xml))
			doc, err := decoder.Decode()
			if err != nil {
				t.Fatalf("Failed to parse XML: %v", err)
			}

			// Extract and build tools
			transitions := extractTransitions(doc)
			sendFunctions := prompt.BuildSendFunctions(transitions)
			tools, mapping := convertToOpenAIToolsWithMapping(sendFunctions)

			// Verify tool count
			if len(tools) != len(tt.expectedTools) {
				t.Errorf("Expected %d tools, got %d", len(tt.expectedTools), len(tools))
			}

			// Verify each expected tool exists
			foundTools := make(map[string]bool)
			for _, tool := range tools {
				funcName := tool.Function.Value.Name.Value
				foundTools[funcName] = true
				t.Logf("  Tool: %s -> %s", funcName, mapping[funcName])
			}

			for _, expected := range tt.expectedTools {
				if !foundTools[expected] {
					t.Errorf("Expected tool '%s' not found", expected)
				}
			}
		})
	}
}

func TestSchemaAttribute_Detected(t *testing.T) {
	// Verify that transitions with schema attributes are properly detected
	xml := `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml"
       version="1.0">
	<state id="s1">
		<transition event="with.schema" schema='{"type":"object","properties":{"data":{"type":"string"}}}' target="s2"/>
		<transition event="without.schema" target="s3"/>
	</state>
	<state id="s2"/>
	<state id="s3"/>
</scxml>`

	decoder := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse XML: %v", err)
	}

	transitions := extractTransitions(doc)
	if len(transitions) != 2 {
		t.Fatalf("Expected 2 transitions, got %d", len(transitions))
	}

	sendFunctions := prompt.BuildSendFunctions(transitions)

	// Check that one has schema data and one doesn't
	schemaCount := 0
	for _, fn := range sendFunctions {
		if fn.Schema != nil && len(fn.Schema.Properties) > 0 {
			schemaCount++
			t.Logf("Function %s has schema with %d properties", fn.Name, len(fn.Schema.Properties))
		}
	}

	if schemaCount != 1 {
		t.Errorf("Expected 1 function with schema, got %d", schemaCount)
	}
}

func TestSchemaAttribute_ActualContentParsing(t *testing.T) {
	// Load the test SCXML file and verify actual schema content is parsed correctly
	data, err := os.ReadFile("testdata/test_agent.scxml")
	if err != nil {
		t.Fatalf("Failed to read test SCXML file: %v", err)
	}

	// Parse the SCXML document
	decoder := xmldom.NewDecoder(strings.NewReader(string(data)))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse SCXML: %v", err)
	}

	transitions := extractTransitions(doc)
	sendFunctions := prompt.BuildSendFunctions(transitions)

	t.Log("=== Verifying Schema Content from test_agent.scxml ===")

	// Find and verify the user.request schema
	var userRequestFunc *prompt.SendFunction
	for i := range sendFunctions {
		if sendFunctions[i].Name == "send_user.request" {
			userRequestFunc = &sendFunctions[i]
			break
		}
	}
	if userRequestFunc == nil {
		t.Fatal("send_user.request function not found")
	}

	if userRequestFunc.Schema == nil || userRequestFunc.Schema.Properties["data"] == nil {
		t.Fatal("user.request should have a data schema")
	}

	dataSchema := userRequestFunc.Schema.Properties["data"]

	t.Logf("user.request data schema has %d properties", len(dataSchema.Properties))

	// Verify the schema has the expected properties from test_agent.scxml
	// Expected: {"query":{"type":"string"},"context":{"type":"object"}}
	queryProp, hasQuery := dataSchema.Properties["query"]
	if !hasQuery {
		t.Error("Expected 'query' property in user.request schema")
	} else if queryProp.Type != "string" {
		t.Errorf("Expected query type 'string', got '%s'", queryProp.Type)
	} else {
		t.Log("✓ query property is correctly typed as string")
	}

	contextProp, hasContext := dataSchema.Properties["context"]
	if !hasContext {
		t.Error("Expected 'context' property in user.request schema")
	} else if contextProp.Type != "object" {
		t.Errorf("Expected context type 'object', got '%s'", contextProp.Type)
	} else {
		t.Log("✓ context property is correctly typed as object")
	}

	// Verify required fields
	if len(dataSchema.Required) != 1 || dataSchema.Required[0] != "query" {
		t.Errorf("Expected required=['query'], got %v", dataSchema.Required)
	} else {
		t.Log("✓ required fields correctly set to ['query']")
	}

	// Find and verify task.complete schema
	var taskCompleteFunc *prompt.SendFunction
	for i := range sendFunctions {
		if sendFunctions[i].Name == "send_task.complete" {
			taskCompleteFunc = &sendFunctions[i]
			break
		}
	}
	if taskCompleteFunc == nil {
		t.Fatal("send_task.complete function not found")
	}

	if taskCompleteFunc.Schema == nil || taskCompleteFunc.Schema.Properties["data"] == nil {
		t.Fatal("task.complete should have a data schema")
	}

	taskDataSchema := taskCompleteFunc.Schema.Properties["data"]

	t.Logf("task.complete data schema has %d properties", len(taskDataSchema.Properties))

	// Expected: {"result":{"type":"string"},"confidence":{"type":"number"}}
	resultProp, hasResult := taskDataSchema.Properties["result"]
	if !hasResult {
		t.Error("Expected 'result' property in task.complete schema")
	} else if resultProp.Type != "string" {
		t.Errorf("Expected result type 'string', got '%s'", resultProp.Type)
	} else {
		t.Log("✓ result property is correctly typed as string")
	}

	confidenceProp, hasConfidence := taskDataSchema.Properties["confidence"]
	if !hasConfidence {
		t.Error("Expected 'confidence' property in task.complete schema")
	} else if confidenceProp.Type != "number" {
		t.Errorf("Expected confidence type 'number', got '%s'", confidenceProp.Type)
	} else {
		t.Log("✓ confidence property is correctly typed as number")
	}

	// Verify required fields
	if len(taskDataSchema.Required) != 1 || taskDataSchema.Required[0] != "result" {
		t.Errorf("Expected required=['result'], got %v", taskDataSchema.Required)
	} else {
		t.Log("✓ required fields correctly set to ['result']")
	}

	// Find and verify task.failed schema
	var taskFailedFunc *prompt.SendFunction
	for i := range sendFunctions {
		if sendFunctions[i].Name == "send_task.failed" {
			taskFailedFunc = &sendFunctions[i]
			break
		}
	}
	if taskFailedFunc == nil {
		t.Fatal("send_task.failed function not found")
	}

	if taskFailedFunc.Schema == nil || taskFailedFunc.Schema.Properties["data"] == nil {
		t.Fatal("task.failed should have a data schema")
	}

	failedDataSchema := taskFailedFunc.Schema.Properties["data"]

	t.Logf("task.failed data schema has %d properties", len(failedDataSchema.Properties))

	// Expected: {"error":{"type":"string"},"retry":{"type":"boolean"}}
	errorProp, hasError := failedDataSchema.Properties["error"]
	if !hasError {
		t.Error("Expected 'error' property in task.failed schema")
	} else if errorProp.Type != "string" {
		t.Errorf("Expected error type 'string', got '%s'", errorProp.Type)
	} else {
		t.Log("✓ error property is correctly typed as string")
	}

	retryProp, hasRetry := failedDataSchema.Properties["retry"]
	if !hasRetry {
		t.Error("Expected 'retry' property in task.failed schema")
	} else if retryProp.Type != "boolean" {
		t.Errorf("Expected retry type 'boolean', got '%s'", retryProp.Type)
	} else {
		t.Log("✓ retry property is correctly typed as boolean")
	}

	// Verify events without schemas don't have data properties
	var shutdownFunc *prompt.SendFunction
	for i := range sendFunctions {
		if sendFunctions[i].Name == "send_system.shutdown" {
			shutdownFunc = &sendFunctions[i]
			break
		}
	}
	if shutdownFunc == nil {
		t.Fatal("send_system.shutdown function not found")
	}

	if shutdownFunc.Schema != nil && shutdownFunc.Schema.Properties["data"] != nil {
		t.Error("system.shutdown should not have a data schema (no schema attribute)")
	} else {
		t.Log("✓ system.shutdown correctly has no data schema")
	}

	t.Log("=== Schema Content Parsing Verification Complete ===")
}

func TestOpenAIToolSchemaConversion(t *testing.T) {
	// Test that parsed schemas are correctly converted to OpenAI tool parameters
	data, err := os.ReadFile("testdata/test_agent.scxml")
	if err != nil {
		t.Fatalf("Failed to read test SCXML file: %v", err)
	}

	decoder := xmldom.NewDecoder(strings.NewReader(string(data)))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse SCXML: %v", err)
	}

	transitions := extractTransitions(doc)
	sendFunctions := prompt.BuildSendFunctions(transitions)
	tools, mapping := convertToOpenAIToolsWithMapping(sendFunctions)

	t.Log("=== Verifying OpenAI Tool Schema Conversion ===")

	// Find the user_request tool
	var userRequestToolIdx = -1
	for i := range tools {
		if tools[i].Function.Value.Name.Value == "send_user_request" {
			userRequestToolIdx = i
			break
		}
	}
	if userRequestToolIdx == -1 {
		t.Fatal("send_user_request tool not found")
	}
	userRequestTool := &tools[userRequestToolIdx]

	// Verify the mapping
	if mapping["send_user_request"] != "user.request" {
		t.Errorf("Expected mapping send_user_request -> user.request, got %s", mapping["send_user_request"])
	}

	// Extract parameters
	params := userRequestTool.Function.Value.Parameters.Value
	t.Logf("Tool parameters: %+v", params)

	// Verify it's an object type
	if params["type"] != "object" {
		t.Errorf("Expected type 'object', got %v", params["type"])
	}

	// Get properties
	propsRaw, ok := params["properties"]
	if !ok {
		t.Fatal("Expected 'properties' in parameters")
	}
	props, ok := propsRaw.(map[string]any)
	if !ok {
		t.Fatalf("Expected properties to be map[string]any, got %T", propsRaw)
	}

	// Verify target and delay are present (added by convertToOpenAIToolsWithMapping)
	if _, hasTarget := props["target"]; !hasTarget {
		t.Error("Expected 'target' property in tool parameters")
	}
	if _, hasDelay := props["delay"]; !hasDelay {
		t.Error("Expected 'delay' property in tool parameters")
	}

	// Verify data property exists and has the parsed schema
	dataRaw, hasData := props["data"]
	if !hasData {
		t.Fatal("Expected 'data' property in tool parameters")
	}
	dataProps, ok := dataRaw.(map[string]any)
	if !ok {
		t.Fatalf("Expected data to be map[string]any, got %T", dataRaw)
	}

	t.Logf("Data schema: %+v", dataProps)

	// Verify data is an object
	dataType, ok := dataProps["type"].(string)
	if !ok {
		t.Errorf("Expected data type to be string, got %T", dataProps["type"])
	} else if dataType != "object" {
		t.Errorf("Expected data type 'object', got '%s'", dataType)
	} else {
		t.Log("✓ data property correctly typed as object")
	}

	// Verify data has properties from the SCXML schema
	dataPropertiesRaw, ok := dataProps["properties"]
	if !ok {
		t.Fatal("Expected 'properties' in data schema")
	}
	dataProperties, ok := dataPropertiesRaw.(map[string]any)
	if !ok {
		t.Fatalf("Expected data properties to be map[string]any, got %T", dataPropertiesRaw)
	}

	// Verify query property
	queryRaw, hasQuery := dataProperties["query"]
	if !hasQuery {
		t.Error("Expected 'query' property in data schema")
	} else {
		query, ok := queryRaw.(map[string]any)
		if !ok {
			t.Errorf("Expected query to be map[string]any, got %T", queryRaw)
		} else {
			queryType, ok := query["type"].(string)
			if !ok || queryType != "string" {
				t.Errorf("Expected query type 'string', got %v", query["type"])
			} else {
				t.Log("✓ query property correctly typed as string")
			}
		}
	}

	// Verify context property
	contextRaw, hasContext := dataProperties["context"]
	if !hasContext {
		t.Error("Expected 'context' property in data schema")
	} else {
		context, ok := contextRaw.(map[string]any)
		if !ok {
			t.Errorf("Expected context to be map[string]any, got %T", contextRaw)
		} else {
			contextType, ok := context["type"].(string)
			if !ok || contextType != "object" {
				t.Errorf("Expected context type 'object', got %v", context["type"])
			} else {
				t.Log("✓ context property correctly typed as object")
			}
		}
	}

	// Verify required fields
	requiredRaw, hasRequired := dataProps["required"]
	if !hasRequired {
		t.Error("Expected 'required' in data schema")
	} else {
		required, ok := requiredRaw.([]string)
		if !ok {
			t.Errorf("Expected required to be []string, got %T", requiredRaw)
		} else if len(required) != 1 || required[0] != "query" {
			t.Errorf("Expected required=['query'], got %v", required)
		} else {
			t.Log("✓ required fields correctly set to ['query']")
		}
	}

	// Find and verify task_complete tool
	var taskCompleteToolIdx = -1
	for i := range tools {
		if tools[i].Function.Value.Name.Value == "send_task_complete" {
			taskCompleteToolIdx = i
			break
		}
	}
	if taskCompleteToolIdx == -1 {
		t.Fatal("send_task_complete tool not found")
	}
	taskCompleteTool := &tools[taskCompleteToolIdx]

	taskParams := taskCompleteTool.Function.Value.Parameters.Value
	taskProps := taskParams["properties"].(map[string]any)
	taskData := taskProps["data"].(map[string]any)
	taskDataProps := taskData["properties"].(map[string]any)

	// Verify result and confidence properties
	resultProp := taskDataProps["result"].(map[string]any)
	resultType, ok := resultProp["type"].(string)
	if !ok || resultType != "string" {
		t.Errorf("Expected result type 'string', got %v", resultProp["type"])
	} else {
		t.Log("✓ task.complete result property correctly typed as string")
	}

	confidenceProp := taskDataProps["confidence"].(map[string]any)
	confidenceType, ok := confidenceProp["type"].(string)
	if !ok || confidenceType != "number" {
		t.Errorf("Expected confidence type 'number', got %v", confidenceProp["type"])
	} else {
		t.Log("✓ task.complete confidence property correctly typed as number")
	}

	// Verify task.failed has boolean type
	var taskFailedToolIdx = -1
	for i := range tools {
		if tools[i].Function.Value.Name.Value == "send_task_failed" {
			taskFailedToolIdx = i
			break
		}
	}
	if taskFailedToolIdx == -1 {
		t.Fatal("send_task_failed tool not found")
	}
	taskFailedTool := &tools[taskFailedToolIdx]

	failedParams := taskFailedTool.Function.Value.Parameters.Value
	failedProps := failedParams["properties"].(map[string]any)
	failedData := failedProps["data"].(map[string]any)
	failedDataProps := failedData["properties"].(map[string]any)

	retryProp := failedDataProps["retry"].(map[string]any)
	retryType, ok := retryProp["type"].(string)
	if !ok || retryType != "boolean" {
		t.Errorf("Expected retry type 'boolean', got %v", retryProp["type"])
	} else {
		t.Log("✓ task.failed retry property correctly typed as boolean")
	}

	t.Log("=== OpenAI Tool Schema Conversion Complete ===")
}

func TestOpenAIToolSchemaConversion_ArrayWithoutItems(t *testing.T) {
	// Test that arrays without items get a fallback items schema for OpenAI
	xml := `<?xml version="1.0"?>
<scxml xmlns="http://www.w3.org/2005/07/scxml" version="1.0">
	<state id="s1">
		<transition event="array.test"
		            schema='{"type":"object","properties":{"failed":{"type":"array"},"tags":{"type":"array","items":{"type":"string"}}}}'
		            target="s2"/>
	</state>
</scxml>`

	decoder := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := decoder.Decode()
	if err != nil {
		t.Fatalf("Failed to parse XML: %v", err)
	}

	transitions := extractTransitions(doc)
	sendFunctions := prompt.BuildSendFunctions(transitions)
	tools, _ := convertToOpenAIToolsWithMapping(sendFunctions)

	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := &tools[0]
	params := tool.Function.Value.Parameters.Value
	props := params["properties"].(map[string]any)
	dataProps := props["data"].(map[string]any)
	dataProperties := dataProps["properties"].(map[string]any)

	// Verify 'failed' array without items gets a fallback
	failedRaw, hasF := dataProperties["failed"]
	if !hasF {
		t.Fatal("Expected 'failed' property")
	}
	failed := failedRaw.(map[string]any)

	failedType, ok := failed["type"].(string)
	if !ok || failedType != "array" {
		t.Errorf("Expected failed type 'array', got %v", failed["type"])
	}

	// OpenAI requires items for arrays - check we have a fallback
	failedItems, hasItems := failed["items"]
	if !hasItems {
		t.Error("Expected 'items' in failed array schema (OpenAI requirement)")
	} else {
		t.Logf("✓ failed array has fallback items schema: %+v", failedItems)
	}

	// Verify 'tags' array with items preserves the items schema
	tagsRaw, hasT := dataProperties["tags"]
	if !hasT {
		t.Fatal("Expected 'tags' property")
	}
	tags := tagsRaw.(map[string]any)

	tagsType, ok := tags["type"].(string)
	if !ok || tagsType != "array" {
		t.Errorf("Expected tags type 'array', got %v", tags["type"])
	}

	tagsItems, hasItems := tags["items"]
	if !hasItems {
		t.Error("Expected 'items' in tags array schema")
	} else {
		tagsItemsMap := tagsItems.(map[string]any)
		itemType, ok := tagsItemsMap["type"].(string)
		if !ok || itemType != "string" {
			t.Errorf("Expected tags items type 'string', got %v", tagsItemsMap["type"])
		} else {
			t.Log("✓ tags array items correctly preserved as string")
		}
	}

	t.Log("=== Array Schema Handling Complete ===")
}
