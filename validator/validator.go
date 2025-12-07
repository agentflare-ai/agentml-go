package validator

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/agentflare-ai/go-jsonschema"
	"github.com/agentflare-ai/go-xmldom"
	"github.com/agentflare-ai/go-xsd"
)

var (
	validatorPool = sync.Pool{
		New: func() any {
			return &Validator{}
		},
	}
)

// Severity represents the severity level of a diagnostic
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Position contains source position information for a diagnostic
type Position struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Offset int64  `json:"offset"`
}

// Related points to a related location in the source (e.g., reference target)
// that can help explain or remedy an error.
type Related struct {
	Label    string   `json:"label"`
	Position Position `json:"position"`
}

// Diagnostic describes a validation issue found in the document
// It is designed to be useful to both humans and LLMs.
type Diagnostic struct {
	Severity  Severity  `json:"severity"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Position  Position  `json:"position"`
	Tag       string    `json:"tag"`
	Attribute string    `json:"attribute,omitempty"`
	SpecRef   string    `json:"spec_ref,omitempty"`
	Hints     []string  `json:"hints,omitempty"`
	Related   []Related `json:"related,omitempty"`
}

// Result is the aggregate validation result
type Result struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// HasErrors returns true if there is at least one error severity diagnostic
func (r *Result) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Add appends diagnostics to the result
func (r *Result) Add(diags ...Diagnostic) {
	r.Diagnostics = append(r.Diagnostics, diags...)
}

// SchemaLoaderSpec defines a schema loader with its matching pattern
type SchemaLoaderSpec struct {
	Pattern string               // Regex pattern to match namespace URIs
	Loader  xsd.SchemaLoaderFunc // Function to load the schema
}

// Config controls validator behavior
type Config struct {
	Strict     bool   // Treat selected warnings as errors; apply stricter SCXML rules
	DataModel  string // Optional datamodel context (ecmascript, xpath, null, starlark)
	SourceName string // Optional source name for reporting

	// RecursiveInvoke enables recursive validation of invoked SCXML files.
	// When true, the validator will attempt to load and validate any SCXML files
	// referenced in <invoke type="scxml" src="..."> elements.
	RecursiveInvoke bool

	// InvokeBasePath is the base directory to resolve relative paths in invoke src attributes.
	// If empty, paths are resolved relative to the current working directory.
	InvokeBasePath string

	// SchemaBasePath is the base directory to resolve relative schema paths in xmlns declarations.
	// If empty, defaults to InvokeBasePath or current working directory.
	SchemaBasePath string

	// SemanticRules allows injection of custom semantic validators.
	// If nil, DefaultSemanticRules() is used.
	// Set to empty slice to disable semantic validation.
	SemanticRules []SemanticRule

	// SchemaLoaders allows injection of custom XSD schema loaders.
	// Loaders are tried in order - more specific patterns should come first.
	// If nil, default loaders are used.
	SchemaLoaders []SchemaLoaderSpec

	// JSONSchemas stores loaded JSON schemas keyed by namespace prefix.
	// These are loaded from schema:* attributes on the root element.
	// Example: schema:user="file://schema.json" -> JSONSchemas["user"] = loaded schema
	JSONSchemas map[string]*jsonschema.Schema

	// visitedFiles tracks already validated files to prevent infinite recursion
	visitedFiles map[string]bool
}

// Validator validates SCXML documents
type Validator struct {
	config Config
}

// New creates a new Validator
func New(cfg ...Config) *Validator {
	c := Config{}
	for _, x := range cfg {
		c = x
	}
	return &Validator{config: c}
}

func ValidateDocument(ctx context.Context, doc xmldom.Document, source string) Result {
	v := validatorPool.Get().(*Validator)
	defer validatorPool.Put(v)
	return v.ValidateDocument(ctx, doc, source)
}

// ValidateString validates an SCXML string and returns diagnostics and the parsed document
func (v *Validator) ValidateString(ctx context.Context, xml string) (Result, xmldom.Document, error) {
	decoder := xmldom.NewDecoderFromBytes([]byte(xml))
	doc, err := decoder.Decode()
	if err != nil {
		return Result{}, nil, fmt.Errorf("failed to parse XML: %w", err)
	}
	return v.ValidateDocument(ctx, doc, xml), doc, nil
}

// ValidateReader reads all from r, validates, and returns result+doc
func (v *Validator) ValidateReader(ctx context.Context, r io.Reader) (Result, xmldom.Document, string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Result{}, nil, "", fmt.Errorf("failed to read input: %w", err)
	}
	decoder := xmldom.NewDecoderFromBytes(data)
	doc, err := decoder.Decode()
	if err != nil {
		return Result{}, nil, string(data), fmt.Errorf("failed to parse XML: %w", err)
	}
	res := v.ValidateDocument(ctx, doc, string(data))
	return res, doc, string(data), nil
}

// ValidateDocument runs the rule set on the provided document
// source should be the raw XML string used to generate doc (for precise positions)
func (v *Validator) ValidateDocument(ctx context.Context, doc xmldom.Document, source string) Result {
	res := Result{}
	if doc == nil {
		res.Add(Diagnostic{Severity: SeverityError, Code: "E000", Message: "nil document", Position: Position{File: v.config.SourceName}})
		return res
	}

	root := doc.DocumentElement()
	if root == nil {
		res.Add(Diagnostic{Severity: SeverityError, Code: "E001", Message: "document has no root element", Position: Position{File: v.config.SourceName}})
		return res
	}

	// Use XSD-based validation
	xsdVal, err := newXSDValidator(v.config)
	if err != nil {
		res.Add(Diagnostic{
			Severity: SeverityError,
			Code:     "E002",
			Message:  fmt.Sprintf("failed to create XSD validator: %v", err),
			Position: Position{File: v.config.SourceName},
		})
		return res
	}

	// Perform XSD validation
	diagnostics := xsdVal.validate(ctx, doc, source)

	// Run semantic validation (dependency injection pattern)
	rules := v.config.SemanticRules
	if rules == nil {
		rules = DefaultSemanticRules()
	}

	for _, rule := range rules {
		semanticDiags := rule.Validate(doc, v.config)
		diagnostics = append(diagnostics, semanticDiags...)
	}

	// Enhance diagnostics with fuzzy matching and helpful hints
	enhanced := enhanceDiagnostics(doc, diagnostics)
	res.Add(enhanced...)

	// If strict, escalate selected warnings to errors
	if v.config.Strict {
		for i := range res.Diagnostics {
			if res.Diagnostics[i].Severity == SeverityWarning {
				res.Diagnostics[i].Severity = SeverityError
			}
		}
	}

	// Recursively validate invoked SCXML files if enabled
	if v.config.RecursiveInvoke {
		invokedDiags := v.validateInvokedSCXML(ctx, doc)
		res.Add(invokedDiags...)
	}

	return res
}

// validateInvokedSCXML recursively validates SCXML files referenced in invoke elements
func (v *Validator) validateInvokedSCXML(ctx context.Context, doc xmldom.Document) []Diagnostic {
	if doc == nil {
		return nil
	}

	// Initialize visited files map if needed
	if v.config.visitedFiles == nil {
		v.config.visitedFiles = make(map[string]bool)
	}

	var diags []Diagnostic
	root := doc.DocumentElement()
	if root == nil {
		return nil
	}

	// Find all invoke elements
	invokes := v.findInvokeElements(root)

	for _, invoke := range invokes {
		// Check if it's an SCXML invocation
		typeAttr := strings.TrimSpace(string(invoke.GetAttribute("type")))
		if typeAttr != "scxml" && typeAttr != "http://www.w3.org/TR/scxml/" {
			continue
		}

		// Get the src attribute
		src := strings.TrimSpace(string(invoke.GetAttribute("src")))
		if src == "" {
			continue // No external file to validate
		}

		// Resolve the path
		filePath := src
		if !filepath.IsAbs(filePath) && v.config.InvokeBasePath != "" {
			filePath = filepath.Join(v.config.InvokeBasePath, src)
		}

		// Get absolute path for tracking
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			absPath = filePath
		}

		// Check if we've already validated this file
		if v.config.visitedFiles[absPath] {
			continue // Prevent infinite recursion
		}
		v.config.visitedFiles[absPath] = true

		// Try to read the invoked SCXML file
		data, err := os.ReadFile(filePath)
		if err != nil {
			// Report as a warning - the file might be generated at runtime
			line, col, _ := invoke.Position()
			diags = append(diags, Diagnostic{
				Severity:  SeverityWarning,
				Code:      "W500",
				Message:   fmt.Sprintf("cannot validate invoked SCXML file %q: %v", src, err),
				Position:  Position{File: v.config.SourceName, Line: line, Column: col},
				Tag:       "invoke",
				Attribute: "src",
				Hints:     []string{"File may be generated at runtime or path may be incorrect"},
			})
			continue
		}

		// Parse the invoked SCXML
		decoder := xmldom.NewDecoderFromBytes(data)
		invokedDoc, err := decoder.Decode()
		if err != nil {
			line, col, _ := invoke.Position()
			diags = append(diags, Diagnostic{
				Severity:  SeverityError,
				Code:      "E501",
				Message:   fmt.Sprintf("invoked SCXML file %q has XML parsing errors: %v", src, err),
				Position:  Position{File: v.config.SourceName, Line: line, Column: col},
				Tag:       "invoke",
				Attribute: "src",
			})
			continue
		}

		// Create a new validator with the same config for the invoked file
		invokedConfig := v.config
		invokedConfig.SourceName = src
		invokedConfig.InvokeBasePath = filepath.Dir(filePath)
		invokedValidator := &Validator{config: invokedConfig}

		// Validate the invoked document
		invokedResult := invokedValidator.ValidateDocument(ctx, invokedDoc, string(data))

		// Add diagnostics from the invoked file
		for _, d := range invokedResult.Diagnostics {
			// Update the file reference to show it's from an invoked file
			if d.Position.File == "" {
				d.Position.File = src
			}
			diags = append(diags, d)
		}

		// Add an info message about successful validation
		if !invokedResult.HasErrors() {
			line, col, _ := invoke.Position()
			diags = append(diags, Diagnostic{
				Severity:  SeverityInfo,
				Code:      "I500",
				Message:   fmt.Sprintf("invoked SCXML file %q validated successfully", src),
				Position:  Position{File: v.config.SourceName, Line: line, Column: col},
				Tag:       "invoke",
				Attribute: "src",
			})
		}
	}

	return diags
}

// findInvokeElements recursively finds all invoke elements in the document
func (v *Validator) findInvokeElements(root xmldom.Element) []xmldom.Element {
	var invokes []xmldom.Element
	var rec func(xmldom.Element)
	rec = func(e xmldom.Element) {
		if strings.ToLower(string(e.TagName())) == "invoke" {
			invokes = append(invokes, e)
		}
		children := e.Children()
		for i := uint(0); i < children.Length(); i++ {
			child := children.Item(i)
			if child != nil {
				rec(child)
			}
		}
	}
	if root != nil {
		rec(root)
	}
	return invokes
}
