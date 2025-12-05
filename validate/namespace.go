package validate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/agentmlx/validator"
	"github.com/agentflare-ai/go-xmldom"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const NamespaceURI = "github.com/agentflare-ai/agentml-go/validate"

type Namespace struct {
	itp agentml.Interpreter
}

func (n *Namespace) URI() string { return NamespaceURI }

func (n *Namespace) Unload(ctx context.Context) error { return nil }

// ValidationResult represents the result of validating AML content
type ValidationResult struct {
	Valid        bool                 `json:"valid"`
	ErrorCount   int                  `json:"error_count"`
	WarningCount int                  `json:"warning_count"`
	InfoCount    int                  `json:"info_count"`
	Diagnostics  []agentml.Diagnostic `json:"diagnostics"`
}

func (n *Namespace) Handle(ctx context.Context, el xmldom.Element) (bool, error) {
	if el == nil {
		return false, fmt.Errorf("validate: element cannot be nil")
	}
	local := strings.ToLower(string(el.LocalName()))
	switch local {
	case "content":
		return true, n.execValidate(ctx, el)
	default:
		return false, nil
	}
}

func (n *Namespace) execValidate(ctx context.Context, el xmldom.Element) error {
	tr := otel.Tracer("validate")
	ctx, span := tr.Start(ctx, "validate.content")
	defer span.End()

	slog.Debug("[VALIDATE] Starting validation", "location", el.GetAttribute("location"))

	dm := n.itp.DataModel()
	if dm == nil {
		slog.Debug("[VALIDATE] No data model available")
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "No data model available for validate",
			Data:      map[string]any{"element": "content"},
			Cause:     fmt.Errorf("no datamodel"),
		}
	}

	// Get AML content - either from content attribute or contentexpr
	var xmlContent string
	var sourceName string

	// Try content attribute first (inline content)
	content := strings.TrimSpace(string(el.GetAttribute("content")))
	if content != "" {
		xmlContent = content
		sourceName = "inline-content"
		span.SetAttributes(attribute.String("validate.source", "content"))
		slog.Debug("[VALIDATE] Using inline content", "bytes", len(xmlContent))
	} else {
		// Try contentexpr for dynamic content from data model
		contentExpr := strings.TrimSpace(string(el.GetAttribute("contentexpr")))
		if contentExpr != "" {
			slog.Debug("[VALIDATE] Evaluating contentexpr", "expr", contentExpr)
			val, err := dm.EvaluateValue(ctx, contentExpr)
			if err != nil {
				slog.Debug("[VALIDATE] Failed to evaluate contentexpr", "error", err)
				return &agentml.PlatformError{
					EventName: "error.execution",
					Message:   "Failed to evaluate contentexpr",
					Data:      map[string]any{"element": "content", "contentexpr": contentExpr},
					Cause:     err,
				}
			}
			if s, ok := val.(string); ok {
				xmlContent = s
				sourceName = "contentexpr"
				span.SetAttributes(attribute.String("validate.source", "contentexpr"))
				slog.Debug("[VALIDATE] Got content from expression", "bytes", len(xmlContent))
			}
		}
	}

	if xmlContent == "" {
		slog.Debug("[VALIDATE] No content to validate")
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "validate:content requires content or contentexpr attribute",
			Data:      map[string]any{"element": "content"},
			Cause:     fmt.Errorf("missing content"),
		}
	}

	// Get the location attribute where we should store the result (required)
	loc := strings.TrimSpace(string(el.GetAttribute("location")))
	if loc == "" {
		slog.Debug("[VALIDATE] No location specified")
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "validate:content requires location attribute",
			Data:      map[string]any{"element": "content", "source": sourceName},
			Cause:     fmt.Errorf("missing location"),
		}
	}

	// Parse optional attributes
	strict := false
	if strictAttr := string(el.GetAttribute("strict")); strictAttr != "" {
		if parsed, err := strconv.ParseBool(strictAttr); err == nil {
			strict = parsed
		}
	}

	recursive := false
	if recursiveAttr := string(el.GetAttribute("recursive")); recursiveAttr != "" {
		if parsed, err := strconv.ParseBool(recursiveAttr); err == nil {
			recursive = parsed
		}
	}

	slog.Debug("[VALIDATE] Creating validator", "source", sourceName, "strict", strict)

	span.SetAttributes(
		attribute.Bool("validate.strict", strict),
		attribute.Bool("validate.recursive", recursive),
	)

	// For content validation, use current working directory for schema resolution
	basePath := "."
	if cwd, err := os.Getwd(); err == nil {
		basePath = cwd
	}
	slog.Debug("[VALIDATE] Using base path", "path", basePath)

	// Create validator with appropriate config
	cfg := validator.Config{
		SourceName:      sourceName,
		Strict:          strict,
		RecursiveInvoke: recursive,
		InvokeBasePath:  basePath,
		SchemaBasePath:  basePath,
	}

	v := validator.New(cfg)
	slog.Debug("[VALIDATE] Created validator, calling ValidateString")

	// Validate
	result, _, err := v.ValidateString(ctx, xmlContent)
	if err != nil {
		slog.Debug("[VALIDATE] ValidateString failed", "error", err)
		// Even on failure, create a validation result with error info
		validationResult := ValidationResult{
			Valid:        false,
			ErrorCount:   1,
			WarningCount: 0,
			InfoCount:    0,
			Diagnostics: []agentml.Diagnostic{{
				Severity: agentml.SeverityError,
				Code:     "VALIDATION_ERROR",
				Message:  err.Error(),
			}},
		}

		// Store the result even on failure
		if setErr := dm.SetVariable(ctx, loc, validationResult); setErr != nil {
			slog.Debug("[VALIDATE] Failed to store error result", "error", setErr)
		}

		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "Validation failed",
			Data:      map[string]any{"element": "content", "source": sourceName, "location": loc},
			Cause:     err,
		}
	}

	slog.Debug("[VALIDATE] ValidateString completed", "diagnostics", len(result.Diagnostics))

	// Count diagnostics by severity
	errorCount := 0
	warningCount := 0
	infoCount := 0
	for _, diag := range result.Diagnostics {
		switch diag.Severity {
		case agentml.SeverityError:
			errorCount++
		case agentml.SeverityWarning:
			warningCount++
		case agentml.SeverityInfo:
			infoCount++
		}
	}

	slog.Debug("[VALIDATE] Validation result", "valid", !result.HasErrors(), "errors", errorCount, "warnings", warningCount, "info", infoCount)

	// Create validation result
	validationResult := ValidationResult{
		Valid:        !result.HasErrors(),
		ErrorCount:   errorCount,
		WarningCount: warningCount,
		InfoCount:    infoCount,
		Diagnostics:  result.Diagnostics,
	}

	slog.Debug("[VALIDATE] Storing result in data model", "location", loc)

	// Store the result in the data model
	if err := dm.SetVariable(ctx, loc, validationResult); err != nil {
		slog.Debug("[VALIDATE] Failed to store result", "error", err)
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "Failed to store validation result",
			Data:      map[string]any{"element": "validate", "source": sourceName, "location": loc},
			Cause:     err,
		}
	}

	slog.Debug("[VALIDATE] Validation completed successfully")

	span.SetAttributes(
		attribute.Bool("validate.valid", validationResult.Valid),
		attribute.Int("validate.errors", errorCount),
		attribute.Int("validate.warnings", warningCount),
		attribute.Int("validate.info", infoCount),
	)

	return nil
}

var _ agentml.Namespace = (*Namespace)(nil)
