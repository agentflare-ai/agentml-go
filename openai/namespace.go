package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/agentml-go/prompt"
	"github.com/agentflare-ai/go-jsonschema"
	"github.com/agentflare-ai/go-pipeline"
	"github.com/agentflare-ai/go-xmldom"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// OpenAINamespaceURI is the XML namespace URI used for OpenAI executable elements.
const OpenAINamespaceURI = "github.com/agentflare-ai/agentml-go/openai"

// ToolCallHandler is called when a tool call is complete and ready to process
type ToolCallHandler func(toolCall openai.ChatCompletionMessageToolCall) error

// Loader returns a NamespaceLoader for the OpenAI namespace.
func Loader() agentml.NamespaceLoader {
	return func(ctx context.Context, itp agentml.Interpreter, doc xmldom.Document) (agentml.Namespace, error) {
		// Create HTTP client with reasonable timeouts
		httpClient := &http.Client{
			Timeout: 90 * time.Second,
			Transport: &http.Transport{
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}

		// Build client options
		var opts []option.RequestOption
		opts = append(opts, option.WithHTTPClient(httpClient))

		if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
			opts = append(opts, option.WithAPIKey(apiKey))
		}

		if baseURL := os.Getenv("OPENAI_BASE_URL"); baseURL != "" {
			slog.Info("Using custom base URL", "baseURL", baseURL)
			opts = append(opts, option.WithBaseURL(baseURL))
		}

		client := openai.NewClient(opts...)
		slog.Info("openai: client created")
		return &ns{itp: itp, client: client}, nil
	}
}

type ns struct {
	itp    agentml.Interpreter
	client openai.Client
}

var _ agentml.Namespace = (*ns)(nil)

func (n *ns) URI() string { return OpenAINamespaceURI }

func (n *ns) Unload(ctx context.Context) error { return nil }

func (n *ns) Handle(ctx context.Context, el xmldom.Element) (bool, error) {
	if el == nil {
		return false, fmt.Errorf("openai: element cannot be nil")
	}
	switch string(el.LocalName()) {
	case "generate":
		slog.Info("openai: handle generate", "el", el)
		return true, n.handleGenerate(ctx, el)
	default:
		return false, nil
	}
}

func (n *ns) handleGenerate(ctx context.Context, el xmldom.Element) error {
	return executeGenerate(ctx, n.itp, n.client, el)
}

// executeGenerate handles <openai:generate> element execution directly.
func executeGenerate(ctx context.Context, interpreter agentml.Interpreter, client openai.Client, el xmldom.Element) error {
	// Extract attributes
	model := string(el.GetAttribute("model"))
	modelExpr := string(el.GetAttribute("modelexpr"))
	promptAttr := string(el.GetAttribute("prompt"))
	promptExpr := string(el.GetAttribute("promptexpr"))
	location := string(el.GetAttribute("location"))
	retryStr := string(el.GetAttribute("retry"))
	reasoning := string(el.GetAttribute("reasoning"))
	maxOutputTokensStr := string(el.GetAttribute("max-output-tokens"))
	retry := 3
	if retryStr != "" {
		if r, err := strconv.Atoi(retryStr); err == nil && r >= 0 {
			retry = r
		}
	}

	var maxOutputTokens *int
	if maxOutputTokensStr != "" {
		if tokens, err := strconv.Atoi(maxOutputTokensStr); err == nil && tokens > 0 {
			maxOutputTokens = &tokens
		}
	}

	if reasoning != "" {
		slog.InfoContext(ctx, "openai: reasoning effort specified", "reasoning", reasoning)
	}
	if maxOutputTokens != nil {
		slog.InfoContext(ctx, "openai: max output tokens specified", "max_output_tokens", *maxOutputTokens)
	}

	// Validate required attributes
	if model == "" && modelExpr == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "Generate element missing required 'model' or 'modelexpr' attribute",
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     fmt.Errorf("generate element missing required 'model' or 'modelexpr' attribute"),
		}
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

	// Support dynamic modelexpr
	modelName := model
	if me := strings.TrimSpace(modelExpr); me != "" {
		v, err := dataModel.EvaluateValue(ctx, me)
		if err == nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				modelName = s
			}
		}
	}

	tracer := otel.Tracer("openai")
	ctx, span := tracer.Start(ctx, "openai.generate.execute",
		trace.WithAttributes(
			attribute.String("openai.model", modelName),
			attribute.String("openai.location", location),
		),
	)
	defer span.End()

	// Evaluate prompt
	promptText, err := evaluatePrompt(ctx, interpreter, promptAttr, el)
	if err != nil {
		span.RecordError(err)
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("Failed to evaluate prompt expression: %v", err),
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     err,
		}
	}

	// Also support dynamic promptexpr
	if pe := strings.TrimSpace(promptExpr); pe != "" {
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

	// Process child <openai:prompt> elements
	childPrompts, err := processChildPrompts(ctx, interpreter, el)
	if err != nil {
		span.RecordError(err)
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("Failed to process child prompt elements: %v", err),
			Data:      map[string]any{"element": "openai:generate", "line": 0},
			Cause:     err,
		}
	}

	finalPrompt := promptText
	if len(childPrompts) > 0 {
		if finalPrompt != "" {
			finalPrompt += "\n" + strings.Join(childPrompts, "\n")
		} else {
			finalPrompt = strings.Join(childPrompts, "\n")
		}
	}

	// Build system instruction from SCXML snapshot
	var systemPrompt string
	var openaiTools []openai.ChatCompletionToolParam
	var eventNameMapping map[string]string
	var sendFunctions []prompt.SendFunction

	if doc, err := interpreter.Snapshot(ctx, agentml.SnapshotConfig{ExcludeData: true}); err == nil {
		transitions := extractTransitions(doc)
		sendFunctions = prompt.BuildSendFunctions(transitions)
		openaiTools, eventNameMapping = convertToOpenAIToolsWithMapping(sendFunctions)
		prompt.PruneSnapshot(doc)

		if b, err2 := xmldom.MarshalIndentWithOptions(doc, "", "  ", true); err2 == nil {
			systemPrompt = string(b)
		}
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
		openai.UserMessage(finalPrompt),
	}

	// Determine tool choice based on whether location is provided
	toolChoice := responses.ToolChoiceOptionsAuto
	if location == "" {
		// When location is omitted, force tool calling for event-based execution
		toolChoice = responses.ToolChoiceOptionsRequired
	}

	// Handle non-tool case (simple chat) - only when location is provided
	if len(openaiTools) == 0 {

		// Simple chat without tools
		if reasoning != "" {
			slog.InfoContext(ctx, "openai: calling Responses API with reasoning", "model", modelName, "reasoning", reasoning)
		}

		// Convert messages to input items for Responses API
		inputItems := convertMessagesToInputItems(messages)

		params := responses.ResponseNewParams{
			Model: shared.ResponsesModel(modelName),
			Input: responses.ResponseNewParamsInputUnion{OfInputItemList: inputItems},
		}

		// Add reasoning configuration if specified
		if reasoning != "" {
			params.Reasoning = shared.ReasoningParam{
				Effort: shared.ReasoningEffort(reasoning),
			}
		}

		// Add max output tokens if specified
		if maxOutputTokens != nil {
			params.MaxOutputTokens = param.NewOpt(int64(*maxOutputTokens))
		}

		response, err := client.Responses.New(ctx, params)
		if err != nil {
			span.RecordError(err)
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to generate content: %v", err),
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     err,
			}
		}

		// Extract content from the Response structure
		var content string
		if len(response.Output) > 0 {
			for _, output := range response.Output {
				if output.Type == "message" {
					message := output.AsMessage()
					if message.Role == "assistant" && len(message.Content) > 0 {
						// Extract text from the first content item
						for _, contentItem := range message.Content {
							if contentItem.Type == "output_text" {
								content = contentItem.Text
								break
							}
						}
					}
					break
				}
			}
		}

		// Log reasoning content if present (for o1 models in Responses API)
		for _, output := range response.Output {
			if output.Type == "reasoning" {
				reasoningItem := output.AsReasoning()
				if reasoningItem.Summary != nil {
					for _, summary := range reasoningItem.Summary {
						if summary.Text != "" {
							slog.InfoContext(ctx, "openai: reasoning content received", "reasoning_length", len(summary.Text))
							if slog.Default().Enabled(ctx, slog.LevelDebug) {
								slog.DebugContext(ctx, "openai: reasoning content", "reasoning", summary.Text)
							}
						}
					}
				}
			}
		}

		if err := dataModel.Assign(ctx, location, content); err != nil {
			span.RecordError(err)
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to assign result to location '%s': %v", location, err),
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     err,
			}
		}
		return nil
	}

	// Tool-based execution - use streaming with pipeline validation
	slog.InfoContext(ctx, "Starting streaming generation with tool calls",
		"model", modelName,
		"num_tools", len(openaiTools),
		"max_retries", retry)

	// Build tool schemas for validation
	toolSchemas := make(map[string]*jsonschema.Schema)
	for _, sendFunc := range sendFunctions {
		if sendFunc.Schema != nil {
			toolSchemas[sendFunc.EventName] = sendFunc.Schema
		}
	}

	// Create pipeline context
	pctx := &StreamingPipelineContext{
		Interpreter: interpreter,
		ToolSchemas: toolSchemas,
		NameMapping: eventNameMapping,
		MaxRetries:  retry,
		RetryCount:  0,
	}

	conversationMessages := make([]openai.ChatCompletionMessageParamUnion, len(messages))
	copy(conversationMessages, messages)

	// Retry loop for handling validation errors
	for retryNum := 0; retryNum < retry; retryNum++ {
		pctx.RetryCount = retryNum

		logAttrs := []any{
			"retry_num", retryNum,
			"model", modelName,
			"num_tools", len(openaiTools),
			"tool_choice", string(toolChoice),
		}
		if reasoning != "" {
			logAttrs = append(logAttrs, "reasoning", reasoning)
		}
		if maxOutputTokens != nil {
			logAttrs = append(logAttrs, "max_output_tokens", *maxOutputTokens)
		}
		slog.InfoContext(ctx, "📡 Calling OpenAI streaming API", logAttrs...)

		// Debug log the messages and tools being sent
		slog.DebugContext(ctx, "OpenAI API request details",
			"model", modelName,
			"num_messages", len(conversationMessages),
			"messages", conversationMessages,
			"num_tools", len(openaiTools),
			"tools", openaiTools,
			"tool_name_mapping", eventNameMapping)

		// Convert messages to input items for Responses API
		inputItems := convertMessagesToInputItems(conversationMessages)

		// Convert ChatCompletionToolParam to ToolUnionParam for Responses API
		responseTools := convertChatToolsToResponseTools(openaiTools)

		streamParams := responses.ResponseNewParams{
			Model: shared.ResponsesModel(modelName),
			Input: responses.ResponseNewParamsInputUnion{OfInputItemList: inputItems},
			Tools: responseTools,
			ToolChoice: responses.ResponseNewParamsToolChoiceUnion{
				OfToolChoiceMode: param.NewOpt(toolChoice),
			},
		}

		// Add reasoning configuration if specified
		if reasoning != "" {
			streamParams.Reasoning = shared.ReasoningParam{
				Effort: shared.ReasoningEffort(reasoning),
			}
		}

		// Add max output tokens if specified
		if maxOutputTokens != nil {
			streamParams.MaxOutputTokens = param.NewOpt(int64(*maxOutputTokens))
		}

		// Track tool calls for error reporting
		var processedToolCalls []*StreamingToolCall
		var streamError error

		// Create handler that processes each tool call immediately as it arrives
		handler := func(tc openai.ChatCompletionMessageToolCall) error {
			streamingTC := &StreamingToolCall{
				Index:        len(processedToolCalls),
				ID:           tc.ID,
				Type:         string(tc.Type),
				FunctionName: tc.Function.Name,
				Arguments:    tc.Function.Arguments,
			}
			processedToolCalls = append(processedToolCalls, streamingTC)

			slog.InfoContext(ctx, "🔍 Processing tool call immediately",
				"function", tc.Function.Name,
				"arguments_length", len(tc.Function.Arguments))

			// Process through validation pipeline immediately
			writer := &ToolCallWriter{}
			p := pipeline.New(ctx,
				jsonDecoderStage,
				createParallelValidatorStage(pctx),
				createToolExecutionStage(pctx),
			)

			if err := p.Process(ctx, writer, streamingTC); err != nil {
				// Check if validation error
				if len(writer.Errors) > 0 {
					streamError = &CorrectionNeededError{Errors: writer.Errors}
				} else {
					streamError = err
				}
				return err // This will interrupt the stream
			}

			slog.InfoContext(ctx, "✅ Tool call validated and executed",
				"function", tc.Function.Name)
			return nil
		}

		// Stream and process tool calls with Harmony parameter for tool use
		slog.InfoContext(ctx, "🎯 Adding Harmony parameter for tool use", "Harmony", "None", "tool_choice", "auto")

		stream := client.Responses.NewStreaming(ctx, streamParams)
		err = processStreamingResponse(ctx, stream, handler)

		// Use streamError if it was set by handler
		if err != nil && streamError != nil {
			err = streamError
		}

		if err != nil && streamError == nil {
			// Stream error (not validation error)
			span.RecordError(err)
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to complete streaming generation: %v", err),
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     err,
			}
		}

		slog.InfoContext(ctx, "📥 Stream complete",
			"num_processed", len(processedToolCalls))

		// Check if correction is needed
		if corrErr, ok := err.(*CorrectionNeededError); ok {
			// Validation failed - retry if we have retries left
			if retryNum < retry-1 {
				slog.WarnContext(ctx, "⚠️  RETRYING GENERATION - Sending correction feedback to LLM",
					"retry_num", retryNum,
					"num_errors", len(corrErr.Errors),
					"retries_remaining", retry-retryNum-1)

				slog.DebugContext(ctx, "Building correction messages for LLM",
					"num_errors", len(corrErr.Errors))

				// Build assistant message with the tool calls that failed
				var toolCallParams []openai.ChatCompletionMessageToolCallParam
				for _, valErr := range corrErr.Errors {
					tc := valErr.ToolCall
					toolCallParams = append(toolCallParams, openai.ChatCompletionMessageToolCallParam{
						ID: tc.ID,
						// Type field will default to "function" automatically
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tc.FunctionName,
							Arguments: tc.Arguments,
						},
					})
				}

				assistantMsg := openai.ChatCompletionAssistantMessageParam{
					// Role field will default to "assistant" automatically
					ToolCalls: toolCallParams,
					// Content is omitted when we have tool calls
				}
				// Convert to union type
				conversationMessages = append(conversationMessages, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsg,
				})

				// Build correction message using the CorrectionStage logic
				correctionStage := CreateCorrectionStage(pctx)
				var correctionMessages []string
				for _, valErr := range corrErr.Errors {
					if corrMsg, err := correctionStage(ctx, valErr); err == nil {
						correctionMessages = append(correctionMessages, corrMsg)
					}
				}

				// Add user message with all corrections
				correctionText := strings.Join(correctionMessages, "\n\n")
				conversationMessages = append(conversationMessages, openai.UserMessage(correctionText))

				slog.DebugContext(ctx, "📤 Sending correction prompt to LLM",
					"correction_length", len(correctionText),
					"will_retry", true)

				continue // Retry
			} else {
				// Max retries reached
				slog.ErrorContext(ctx, "❌ GENERATION FAILED - Max retries reached",
					"max_retries", retry,
					"final_error", err)
				span.RecordError(err)
				return &agentml.PlatformError{
					EventName: "error.execution",
					Message:   fmt.Sprintf("Tool call validation failed after %d retries: %v", retry, err),
					Data:      map[string]any{"element": "openai:generate", "line": 0},
					Cause:     err,
				}
			}
		} else if err != nil {
			// Other error (e.g., JSON decode error, execution error)
			span.RecordError(err)
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("Failed to process streaming tool calls: %v", err),
				Data:      map[string]any{"element": "openai:generate", "line": 0},
				Cause:     err,
			}
		}

		// Success!
		slog.InfoContext(ctx, "✅ GENERATION SUCCESSFUL - All tool calls validated and executed",
			"num_tool_calls", len(processedToolCalls),
			"retry_num", retryNum)
		return nil
	}

	// Should not reach here
	return &agentml.PlatformError{
		EventName: "error.execution",
		Message:   "Unexpected end of retry loop",
		Data:      map[string]any{"element": "openai:generate", "line": 0},
		Cause:     fmt.Errorf("unexpected end of retry loop"),
	}
}

// convertMessagesToInputItems converts ChatCompletion messages to Response input items
func convertMessagesToInputItems(messages []openai.ChatCompletionMessageParamUnion) []responses.ResponseInputItemUnionParam {
	var inputItems []responses.ResponseInputItemUnionParam

	for _, msg := range messages {
		var role string
		var content responses.EasyInputMessageContentUnionParam

		// Extract role and content from message
		if msg.OfUser != nil {
			role = "user"
			content.OfString = param.NewOpt(msg.OfUser.Content.OfString.Value)
		} else if msg.OfAssistant != nil {
			role = "assistant"
			content.OfString = param.NewOpt(msg.OfAssistant.Content.OfString.Value)
		} else if msg.OfSystem != nil {
			role = "system"
			content.OfString = param.NewOpt(msg.OfSystem.Content.OfString.Value)
		} else if msg.OfDeveloper != nil {
			role = "developer"
			content.OfString = param.NewOpt(msg.OfDeveloper.Content.OfString.Value)
		} else {
			continue // Skip unknown message types
		}

		inputItem := responses.EasyInputMessageParam{
			Role:    responses.EasyInputMessageRole(role),
			Content: content,
		}

		inputItems = append(inputItems, responses.ResponseInputItemUnionParam{
			OfMessage: &inputItem,
		})
	}

	return inputItems
}

// processStreamingResponse handles streaming Response events and tool calls
func processStreamingResponse(ctx context.Context, stream *ssestream.Stream[responses.ResponseStreamEventUnion], handler ToolCallHandler) error {
	// Track tool calls as they stream
	toolCallMap := make(map[string]*openai.ChatCompletionMessageToolCall)

	for stream.Next() {
		event := stream.Current()

		// Handle different event types
		switch event.Type {
		case "response.output_item.added":
			// A new output item was added
			outputItemAdded := event.AsResponseOutputItemAdded()
			item := outputItemAdded.Item
			if item.Type == "function_call" {
				functionCall := item.AsFunctionCall()
				// Function call started
				tc := &openai.ChatCompletionMessageToolCall{
					ID: functionCall.CallID,
					Function: openai.ChatCompletionMessageToolCallFunction{
						Name:      functionCall.Name,
						Arguments: functionCall.Arguments,
					},
				}
				toolCallMap[functionCall.CallID] = tc

				slog.Debug("Tool call started",
					"call_id", functionCall.CallID,
					"function", functionCall.Name,
					"arguments_length", len(functionCall.Arguments))
			}

		case "response.output_item.done":
			// An output item is complete
			outputItemDone := event.AsResponseOutputItemDone()
			item := outputItemDone.Item
			if item.Type == "function_call" {
				functionCall := item.AsFunctionCall()
				// Function call completed
				if tc, exists := toolCallMap[functionCall.CallID]; exists {
					slog.Debug("Tool call complete, processing immediately",
						"call_id", functionCall.CallID,
						"function", functionCall.Name,
						"arguments_length", len(functionCall.Arguments))

					// Call handler immediately - if it returns error, interrupt stream
					if err := handler(*tc); err != nil {
						slog.Warn("Handler returned error, interrupting stream",
							"error", err,
							"function", functionCall.Name)
						return err
					}
				}
			}
		case "response.reasoning_text.delta":
			// Reasoning text delta
			reasoningTextDelta := event.AsResponseReasoningSummaryTextDelta()
			slog.Debug("Reasoning text delta",
				"summary_index", reasoningTextDelta.SummaryIndex,
				"text", reasoningTextDelta.Delta)

		case "response.output_text.delta":
			// Text output delta
			textDelta := event.AsResponseOutputTextDelta()
			slog.Debug("Text output delta",
				"content_index", textDelta.ContentIndex,
				"text_length", len(textDelta.Delta))

		case "response.completed":
			// Response is complete
			slog.Debug("Response completed")
			slog.Debug("Response", "response", event.AsResponseCompleted())
			// Could handle final processing here if needed

		default:
			// Log other events for debugging
			slog.Debug("Streaming event", "event", event.Type)
		}
	}

	// Check for stream errors
	if err := stream.Err(); err != nil {
		return err
	}

	return nil
}

// evaluatePrompt evaluates the prompt attribute using the data model if it contains expressions.
func evaluatePrompt(ctx context.Context, interpreter agentml.Interpreter, promptText string, el xmldom.Element) (string, error) {
	if promptText == "" {
		return "", nil
	}

	if !strings.Contains(promptText, "${") && !strings.Contains(promptText, "{{") {
		return promptText, nil
	}

	dataModel := interpreter.DataModel()
	if dataModel == nil {
		return promptText, nil
	}

	result, err := dataModel.EvaluateValue(ctx, promptText)
	if err != nil {
		return promptText, nil
	}

	if str, ok := result.(string); ok {
		return str, nil
	}

	return fmt.Sprintf("%v", result), nil
}

// processChildPrompts processes child <openai:prompt> elements as Go templates.
func processChildPrompts(ctx context.Context, interpreter agentml.Interpreter, el xmldom.Element) ([]string, error) {
	var prompts []string

	children := el.ChildNodes()
	if children.Length() == 0 {
		return prompts, nil
	}

	dataModel := interpreter.DataModel()
	var templateData map[string]any
	if dataModel != nil {
		templateData = make(map[string]any)
	}

	for i := uint(0); i < children.Length(); i++ {
		child := children.Item(i)
		element, ok := child.(xmldom.Element)
		if !ok {
			continue
		}

		localName := element.LocalName()
		namespaceURI := element.NamespaceURI()

		if string(localName) == "prompt" && (string(namespaceURI) == OpenAINamespaceURI || string(namespaceURI) == "") {
			promptContent := string(element.TextContent())
			promptContent = strings.TrimSpace(promptContent)
			if promptContent == "" {
				continue
			}

			if dataModel != nil && (strings.HasPrefix(promptContent, "`") || strings.Contains(promptContent, "${")) {
				result, err := dataModel.EvaluateValue(ctx, promptContent)
				if err != nil {
					slog.Warn("failed to evaluate prompt through data model", "error", err)
				} else if str, ok := result.(string); ok {
					promptContent = str
				}
			}

			processedPrompt, err := processTemplate(promptContent, templateData)
			if err != nil {
				return nil, fmt.Errorf("failed to process template in prompt element: %w", err)
			}

			prompts = append(prompts, processedPrompt)
		}
	}

	return prompts, nil
}

// processTemplate processes a text string as a Go template with the given data.
func processTemplate(templateText string, data map[string]any) (string, error) {
	if !strings.Contains(templateText, "{{") {
		return templateText, nil
	}

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
		return templateText, nil
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return templateText, nil
	}

	return buf.String(), nil
}
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

	// For testing and compatibility, also extract regular transition elements
	// and runtime:send elements if no runtime transitions were found
	if len(transitions) == 0 {
		regularTransitions := root.GetElementsByTagName("transition")
		for i := uint(0); i < regularTransitions.Length(); i++ {
			if elem, ok := regularTransitions.Item(i).(xmldom.Element); ok {
				transitions = append(transitions, elem)
			}
		}

		// Also include runtime:send elements as they represent available events
		runtimeSends := root.GetElementsByTagName("send")
		for i := uint(0); i < runtimeSends.Length(); i++ {
			if elem, ok := runtimeSends.Item(i).(xmldom.Element); ok {
				transitions = append(transitions, elem)
			}
		}
	}

	return transitions
}

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

func convertToOpenAIToolsWithMapping(sendFunctions []prompt.SendFunction) ([]openai.ChatCompletionToolParam, map[string]string) {
	var tools []openai.ChatCompletionToolParam
	mapping := make(map[string]string)

	for _, fn := range sendFunctions {
		// Sanitize function name for OpenAI (dots are not allowed)
		sanitizedName := sanitizeFunctionName(fn.Name)

		// Use the original event name from the SendFunction
		mapping[sanitizedName] = fn.EventName

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
			// Type field will default to "function" automatically
			Function: shared.FunctionDefinitionParam{
				Name:        sanitizedName,
				Description: param.NewOpt(fn.Description),
				Parameters:  shared.FunctionParameters(parameters),
			},
		}

		tools = append(tools, tool)
	}

	return tools, mapping
}

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
		// Type is always "function" in current OpenAI API, no need to check

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

// validateToolCalls validates tool call arguments against their schemas.
func validateToolCalls(sendFunctions []prompt.SendFunction, toolCalls []openai.ChatCompletionMessageToolCall) map[string][]string {
	validationErrors := make(map[string][]string)
	schemaMap := make(map[string]*jsonschema.Schema)
	for _, fn := range sendFunctions {
		sanitizedName := sanitizeFunctionName(fn.Name)
		schemaMap[sanitizedName] = fn.Schema
	}
	for _, toolCall := range toolCalls {
		// Type is always "function" in current OpenAI API, no need to check
		sanitizedName := toolCall.Function.Name
		schema, exists := schemaMap[sanitizedName]
		if !exists {
			validationErrors[sanitizedName] = append(validationErrors[sanitizedName], "unknown function")
			continue
		}
		var arguments map[string]any
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			validationErrors[sanitizedName] = append(validationErrors[sanitizedName], fmt.Sprintf("invalid JSON: %v", err))
			continue
		}
		if schema != nil {
			result := jsonschema.ValidateDocument(arguments, schema)
			if !result.Valid {
				var errorMessages []string
				for _, err := range result.Errors {
					errorMessages = append(errorMessages, fmt.Sprintf("%s: %s", err.Path, err.Message))
				}
				validationErrors[sanitizedName] = errorMessages
			}
		}
	}
	return validationErrors
}

// getMapKeys returns a slice of keys from a map for logging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// convertChatToolsToResponseTools converts ChatCompletionToolParam to ToolUnionParam for Responses API
func convertChatToolsToResponseTools(chatTools []openai.ChatCompletionToolParam) []responses.ToolUnionParam {
	var responseTools []responses.ToolUnionParam

	for _, chatTool := range chatTools {
		// Extract the function definition
		functionDef := chatTool.Function

		// Convert parameters from shared.FunctionParameters to map[string]any
		var parameters map[string]any
		if functionDef.Parameters != nil {
			parameters = map[string]any(functionDef.Parameters)
		} else {
			parameters = map[string]any{}
		}

		// Extract description from optional field
		var description param.Opt[string]
		if functionDef.Description.Valid() {
			description = functionDef.Description
		}

		// Create the function tool with description for Harmony compatibility
		functionTool := responses.FunctionToolParam{
			Name:        functionDef.Name,
			Description: description,
			Parameters:  parameters,
			Strict:      param.NewOpt(false),
		}

		responseTool := responses.ToolUnionParam{
			OfFunction: &functionTool,
		}

		responseTools = append(responseTools, responseTool)
	}

	return responseTools
}
