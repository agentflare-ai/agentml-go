package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-jsonschema"
	"github.com/agentflare-ai/go-pipeline"
	"go.opentelemetry.io/otel"
)

// StreamingToolCall represents a tool call being processed
type StreamingToolCall struct {
	Index        int
	ID           string
	Type         string
	FunctionName string
	Arguments    string
}

// ValidationError represents a tool call validation error
type ValidationError struct {
	ToolCall *StreamingToolCall
	Errors   []string
}

// StreamingPipelineContext holds context for pipeline processing
type StreamingPipelineContext struct {
	Interpreter agentml.Interpreter
	ToolSchemas map[string]*jsonschema.Schema
	NameMapping map[string]string // Maps sanitized names to original event names
	MaxRetries  int
	RetryCount  int
}

// ToolCallWriter accumulates validation results
type ToolCallWriter struct {
	Errors []ValidationError
}

// jsonDecoderStage is a pipeline stage that decodes and validates JSON arguments
func jsonDecoderStage(ctx context.Context, w *ToolCallWriter, input *StreamingToolCall, next pipeline.Next[context.Context, *ToolCallWriter, *StreamingToolCall]) error {
	ctx, span := otel.Tracer("openai.streaming").Start(ctx, "JSONDecoder")
	defer span.End()

	slog.DebugContext(ctx, "Decoding tool call JSON",
		"function", input.FunctionName,
		"arguments_length", len(input.Arguments))

	// Validate JSON can be decoded
	var jsonArgs map[string]any
	decoder := json.NewDecoder(strings.NewReader(input.Arguments))
	if err := decoder.Decode(&jsonArgs); err != nil {
		slog.ErrorContext(ctx, "🛑 GENERATION INTERRUPTED - JSON decode failed",
			"function", input.FunctionName,
			"error", err,
			"arguments", input.Arguments)

		// Record decode error
		w.Errors = append(w.Errors, ValidationError{
			ToolCall: input,
			Errors:   []string{fmt.Sprintf("JSON decode error: %v", err)},
		})

		slog.DebugContext(ctx, "Will retry generation with JSON error correction",
			"function", input.FunctionName)

		// Return error to interrupt pipeline
		return fmt.Errorf("JSON decode failed for tool call %s: %w", input.FunctionName, err)
	}

	slog.DebugContext(ctx, "Successfully decoded tool call JSON",
		"function", input.FunctionName)

	// Continue to next stage
	return next(ctx, w, input)
}

// createParallelValidatorStage creates a pipeline stage that validates against the correct schema for the function
func createParallelValidatorStage(pctx *StreamingPipelineContext) pipeline.Pipe[context.Context, *ToolCallWriter, *StreamingToolCall] {
	return func(ctx context.Context, w *ToolCallWriter, input *StreamingToolCall, next pipeline.Next[context.Context, *ToolCallWriter, *StreamingToolCall]) error {
		ctx, span := otel.Tracer("openai.streaming").Start(ctx, "ParallelValidator")
		defer span.End()

		// Map function name to event name using NameMapping
		originalEventName := pctx.NameMapping[input.FunctionName]
		if originalEventName == "" {
			originalEventName = input.FunctionName
		}

		// Look up the schema for this specific event
		schema, exists := pctx.ToolSchemas[originalEventName]
		if !exists {
			slog.ErrorContext(ctx, "🛑 GENERATION INTERRUPTED - No schema found for event",
				"function", input.FunctionName,
				"event", originalEventName)

			w.Errors = append(w.Errors, ValidationError{
				ToolCall: input,
				Errors:   []string{fmt.Sprintf("no schema found for event '%s'", originalEventName)},
			})

			return fmt.Errorf("no schema found for event %s", originalEventName)
		}

		slog.DebugContext(ctx, "Validating tool call against schema",
			"function", input.FunctionName,
			"event", originalEventName)

		// Parse and validate the arguments JSON against the schema
		var args map[string]any
		if err := json.Unmarshal([]byte(input.Arguments), &args); err != nil {
			slog.ErrorContext(ctx, "🛑 GENERATION INTERRUPTED - JSON parse error",
				"function", input.FunctionName,
				"error", err)

			w.Errors = append(w.Errors, ValidationError{
				ToolCall: input,
				Errors:   []string{fmt.Sprintf("JSON parse error: %v", err)},
			})

			return fmt.Errorf("JSON parse error: %w", err)
		}

		// Validate using ValidateJSONDocument
		result := jsonschema.ValidateJSONDocument(args, schema)
		if !result.Valid {
			slog.ErrorContext(ctx, "🛑 GENERATION INTERRUPTED - Schema validation failed",
				"function", input.FunctionName,
				"event", originalEventName,
				"errors", result.Errors)

			w.Errors = append(w.Errors, ValidationError{
				ToolCall: input,
				Errors:   []string{fmt.Sprintf("%s: %v", originalEventName, result.Errors)},
			})

			return fmt.Errorf("schema validation failed for tool call %s", input.FunctionName)
		}

		// Validation succeeded!
		slog.DebugContext(ctx, "Schema validation succeeded",
			"function", input.FunctionName,
			"event", originalEventName)

		return next(ctx, w, input)
	}
}

// CreateCorrectionStage creates a function that prepares error messages for LLM correction
func CreateCorrectionStage(pctx *StreamingPipelineContext) func(context.Context, ValidationError) (string, error) {
	return func(ctx context.Context, input ValidationError) (string, error) {
		ctx, span := otel.Tracer("openai.streaming").Start(ctx, "CorrectionStage")
		defer span.End()

		// Build comprehensive error message for LLM
		var errorMsg strings.Builder
		errorMsg.WriteString(fmt.Sprintf("Tool call '%s' (ID: %s) failed validation:\n",
			input.ToolCall.FunctionName, input.ToolCall.ID))
		errorMsg.WriteString(fmt.Sprintf("Arguments provided:\n%s\n\n", input.ToolCall.Arguments))
		errorMsg.WriteString("Validation errors:\n")

		for i, err := range input.Errors {
			errorMsg.WriteString(fmt.Sprintf("%d. %s\n", i+1, err))
		}

		errorMsg.WriteString("\nPlease correct the tool call arguments to match the expected schema.")

		slog.DebugContext(ctx, "Prepared correction message",
			"function", input.ToolCall.FunctionName,
			"num_errors", len(input.Errors))

		return errorMsg.String(), nil
	}
}

// createToolExecutionStage creates a pipeline stage that executes validated tool calls
func createToolExecutionStage(pctx *StreamingPipelineContext) pipeline.Pipe[context.Context, *ToolCallWriter, *StreamingToolCall] {
	return func(ctx context.Context, w *ToolCallWriter, input *StreamingToolCall, next pipeline.Next[context.Context, *ToolCallWriter, *StreamingToolCall]) error {
		ctx, span := otel.Tracer("openai.streaming").Start(ctx, "ToolExecution")
		defer span.End()

		slog.InfoContext(ctx, "Executing validated tool call",
			"function", input.FunctionName,
			"id", input.ID)

		// Parse arguments
		var args map[string]any
		if err := json.Unmarshal([]byte(input.Arguments), &args); err != nil {
			return fmt.Errorf("failed to unmarshal arguments: %w", err)
		}

		slog.DebugContext(ctx, "Parsed tool call arguments",
			"function", input.FunctionName,
			"args", args)

		// Map sanitized function name back to original event name
		originalEventName := pctx.NameMapping[input.FunctionName]
		if originalEventName == "" {
			originalEventName = input.FunctionName
		}

		// Extract data, target, and delay from arguments
		var target string
		var delay string
		var eventData map[string]any

		if dataVal, ok := args["data"]; ok {
			if dataMap, ok := dataVal.(map[string]any); ok {
				eventData = dataMap
			}
		}

		slog.DebugContext(ctx, "Extracted event data from arguments",
			"function", input.FunctionName,
			"has_data", eventData != nil,
			"data", eventData)

		if targetVal, ok := args["target"]; ok {
			if targetStr, ok := targetVal.(string); ok {
				target = targetStr
			}
		}

		if delayVal, ok := args["delay"]; ok {
			if delayStr, ok := delayVal.(string); ok {
				delay = delayStr
			}
		}

		// Build event with data
		ev := &agentml.Event{
			Name: originalEventName,
			Type: agentml.EventTypeExternal,
			Data: eventData,
		}
		if target != "" {
			ev.Origin = target
		}
		if delay != "" {
			ev.Delay = delay
		}

		slog.DebugContext(ctx, "Built Event structure before sending",
			"event", ev.Name,
			"target", ev.Origin,
			"has_data", ev.Data != nil,
			"data", ev.Data,
			"delay", ev.Delay)

		// Send the event directly
		slog.DebugContext(ctx, "Sending event to interpreter",
			"event", ev.Name)
		if err := pctx.Interpreter.Send(ctx, ev); err != nil {
			return fmt.Errorf("failed to send event: %w", err)
		}

		slog.InfoContext(ctx, "Successfully executed tool call",
			"function", input.FunctionName,
			"event", originalEventName)

		return next(ctx, w, input)
	}
}

// ProcessStreamingToolCalls processes accumulated tool calls through the validation pipeline
func ProcessStreamingToolCalls(ctx context.Context, pctx *StreamingPipelineContext, toolCalls []*StreamingToolCall) error {
	ctx, span := otel.Tracer("openai.streaming").Start(ctx, "ProcessStreamingToolCalls")
	defer span.End()

	slog.InfoContext(ctx, "Processing streaming tool calls", "count", len(toolCalls))

	// Build the pipeline: Decoder → ParallelValidator → Execution
	p := pipeline.New(ctx,
		jsonDecoderStage,
		createParallelValidatorStage(pctx),
		createToolExecutionStage(pctx),
	)

	writer := &ToolCallWriter{}

	// Process each tool call through the pipeline
	for _, tc := range toolCalls {
		slog.DebugContext(ctx, "Processing tool call", "function", tc.FunctionName)

		if err := p.Process(ctx, writer, tc); err != nil {
			slog.ErrorContext(ctx, "Tool call processing failed",
				"function", tc.FunctionName,
				"error", err)

			// Check if we have validation errors to correct
			if len(writer.Errors) > 0 {
				return &CorrectionNeededError{
					Errors: writer.Errors,
				}
			}

			return err
		}
	}

	slog.InfoContext(ctx, "Successfully processed all tool calls", "count", len(toolCalls))
	return nil
}

// CorrectionNeededError indicates that LLM correction is needed
type CorrectionNeededError struct {
	Errors []ValidationError
}

func (e *CorrectionNeededError) Error() string {
	var buf bytes.Buffer
	buf.WriteString("Tool call validation failed, correction needed:\n")
	for _, valErr := range e.Errors {
		buf.WriteString(fmt.Sprintf("- %s: %s\n", valErr.ToolCall.FunctionName, strings.Join(valErr.Errors, "; ")))
	}
	return buf.String()
}
