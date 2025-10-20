package validate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/agentflare-ai/agentml"
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
	Valid        bool                   `json:"valid"`
	ErrorCount   int                    `json:"error_count"`
	WarningCount int                    `json:"warning_count"`
	InfoCount    int                    `json:"info_count"`
	Diagnostics  []validator.Diagnostic `json:"diagnostics"`
}

func (n *Namespace) Handle(ctx context.Context, el xmldom.Element) (bool, error) {
	if el == nil {
		return false, fmt.Errorf("validate: element cannot be nil")
	}
	local := strings.ToLower(string(el.LocalName()))
	switch local {
	case "validate":
		return true, n.execValidate(ctx, el)
	default:
		return false, nil
	}
}

func (n *Namespace) execValidate(ctx context.Context, el xmldom.Element) error {
	tr := otel.Tracer("validate")
	ctx, span := tr.Start(ctx, "validate.validate")
	defer span.End()

	dm := n.itp.DataModel()
	if dm == nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "No data model available for validate",
			Data:      map[string]any{"element": "validate"},
			Cause:     fmt.Errorf("no datamodel"),
		}
	}

	// Get AML content - either from file (src) or from data model (content)
	var xmlContent string
	var sourceName string

	// Try content attribute first (data model content)
	content := strings.TrimSpace(string(el.GetAttribute("content")))
	if content != "" {
		xmlContent = content
		sourceName = "inline-content"
		span.SetAttributes(attribute.String("validate.source", "content"))
	} else {
		// Try contentexpr for dynamic content from data model
		contentExpr := strings.TrimSpace(string(el.GetAttribute("contentexpr")))
		if contentExpr != "" {
			val, err := dm.EvaluateValue(ctx, contentExpr)
			if err != nil {
				return &agentml.PlatformError{
					EventName: "error.execution",
					Message:   "Failed to evaluate contentexpr",
					Data:      map[string]any{"element": "validate", "contentexpr": contentExpr},
					Cause:     err,
				}
			}
			if s, ok := val.(string); ok {
				xmlContent = s
				sourceName = "contentexpr"
				span.SetAttributes(attribute.String("validate.source", "contentexpr"))
			}
		}
	}

	// If no content provided, try file-based validation
	if xmlContent == "" {
		src := strings.TrimSpace(string(el.GetAttribute("src")))
		if src == "" {
			// Try srcexpr if src is not provided
			srcExpr := strings.TrimSpace(string(el.GetAttribute("srcexpr")))
			if srcExpr != "" {
				val, err := dm.EvaluateValue(ctx, srcExpr)
				if err != nil {
					return &agentml.PlatformError{
						EventName: "error.execution",
						Message:   "Failed to evaluate srcexpr",
						Data:      map[string]any{"element": "validate", "srcexpr": srcExpr},
						Cause:     err,
					}
				}
				if s, ok := val.(string); ok {
					src = s
				}
			}
		}

		if src == "" {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   "validate:validate requires either content/contentexpr or src/srcexpr attribute",
				Data:      map[string]any{"element": "validate"},
				Cause:     fmt.Errorf("missing content or src"),
			}
		}

		span.SetAttributes(attribute.String("validate.src", src))

		// Read the AML file
		xmlData, err := os.ReadFile(src)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   "Failed to read AML file",
				Data:      map[string]any{"element": "validate", "src": src},
				Cause:     err,
			}
		}
		xmlContent = string(xmlData)
		sourceName = src
		span.SetAttributes(attribute.String("validate.source", "file"))
	}

	// Get the location attribute where we should store the result (required)
	loc := strings.TrimSpace(string(el.GetAttribute("location")))
	if loc == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "validate:validate requires location attribute",
			Data:      map[string]any{"element": "validate", "source": sourceName},
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

	span.SetAttributes(
		attribute.Bool("validate.strict", strict),
		attribute.Bool("validate.recursive", recursive),
	)

	// Determine base path for relative schema/invoke resolution
	// For file-based validation, use the file's directory
	// For content-based validation, use current working directory
	var basePath string
	if sourceName != "inline-content" && sourceName != "contentexpr" {
		absPath, err := filepath.Abs(sourceName)
		if err != nil {
			absPath = sourceName
		}
		basePath = filepath.Dir(absPath)
	} else {
		// For content validation, use current working directory
		if cwd, err := os.Getwd(); err == nil {
			basePath = cwd
		} else {
			basePath = "."
		}
	}

	// Create validator with appropriate config
	cfg := validator.Config{
		SourceName:      sourceName,
		Strict:          strict,
		RecursiveInvoke: recursive,
		InvokeBasePath:  basePath,
		SchemaBasePath:  basePath,
	}

	v := validator.New(cfg)

	// Validate
	result, _, err := v.ValidateString(ctx, xmlContent)
	if err != nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "Validation failed",
			Data:      map[string]any{"element": "validate", "source": sourceName, "location": loc},
			Cause:     err,
		}
	}

	// Count diagnostics by severity
	errorCount := 0
	warningCount := 0
	infoCount := 0
	for _, diag := range result.Diagnostics {
		switch diag.Severity {
		case validator.SeverityError:
			errorCount++
		case validator.SeverityWarning:
			warningCount++
		case validator.SeverityInfo:
			infoCount++
		}
	}

	// Create validation result
	validationResult := ValidationResult{
		Valid:        !result.HasErrors(),
		ErrorCount:   errorCount,
		WarningCount: warningCount,
		InfoCount:    infoCount,
		Diagnostics:  result.Diagnostics,
	}

	// Store the result in the data model
	if err := dm.SetVariable(ctx, loc, validationResult); err != nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "Failed to store validation result",
			Data:      map[string]any{"element": "validate", "source": sourceName, "location": loc},
			Cause:     err,
		}
	}

	span.SetAttributes(
		attribute.Bool("validate.valid", validationResult.Valid),
		attribute.Int("validate.errors", errorCount),
		attribute.Int("validate.warnings", warningCount),
		attribute.Int("validate.info", infoCount),
	)

	return nil
}

var _ agentml.Namespace = (*Namespace)(nil)
