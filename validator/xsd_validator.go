package validator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/agentflare-ai/go-jsonschema"
	"github.com/agentflare-ai/go-xmldom"
	"github.com/agentflare-ai/go-xsd"
)

// xsdValidator wraps the XSD validator and converts its output to our format
type xsdValidator struct {
	config Config
}

// newXSDValidator creates a validator using XSD schemas
func newXSDValidator(config Config) (*xsdValidator, error) {
	// Validator is now stateless - schemas are loaded dynamically from xmlns declarations
	return &xsdValidator{
		config: config,
	}, nil
}

// validate performs XSD validation and converts to our diagnostic format
func (v *xsdValidator) validate(_ context.Context, doc xmldom.Document, source string) []Diagnostic {
	// Load schemas dynamically from the document's xmlns declarations
	var schema *xsd.Schema
	var schemaLoadErr error

	// Use SchemaBasePath if set, otherwise fall back to InvokeBasePath
	baseDir := v.config.SchemaBasePath
	if baseDir == "" {
		baseDir = v.config.InvokeBasePath
	}

	// Create schema loader with configured loaders
	// Loaders are tried in order: custom loaders first, then GitHub loader as fallback
	loaders := make([]xsd.PatternLoader, 0)

	// Add custom loaders from config first (higher priority)
	if v.config.SchemaLoaders != nil {
		slog.Debug("Adding custom schema loaders", "count", len(v.config.SchemaLoaders))
		for i, spec := range v.config.SchemaLoaders {
			slog.Debug("Adding custom loader", "index", i, "pattern", spec.Pattern)
			loaders = append(loaders, xsd.PatternLoader{
				Pattern: spec.Pattern,
				Loader:  spec.Loader,
			})
		}
	}

	// Add GitHub loader as fallback for github.com/* namespaces
	loaders = append(loaders, xsd.PatternLoader{
		Pattern: "^github\\.com/.*",
		Loader:  GitHubSchemaLoader(nil),
	})

	loader, err := xsd.NewSchemaLoader(xsd.SchemaLoaderConfig{
		BaseDir: baseDir,
		Loaders: loaders,
	})
	if err != nil {
		return []Diagnostic{{
			Severity: SeverityError,
			Code:     "E001",
			Message:  fmt.Sprintf("Failed to create schema loader: %v", err),
			Position: Position{
				File: v.config.SourceName,
				Line: 1,
			},
		}}
	}

	// Extract namespaces from document
	namespaces := xsd.ExtractNamespaces(doc)

	// Load schemas for all namespaces
	schema, schemaLoadErr = loader.LoadSchemasFromNamespaces(namespaces)
	if schemaLoadErr != nil {
		// Return diagnostic about schema loading failure
		return []Diagnostic{{
			Severity: SeverityError,
			Code:     "E001",
			Message:  fmt.Sprintf("Failed to load schemas from xmlns declarations: %v", schemaLoadErr),
			Position: Position{
				File: v.config.SourceName,
				Line: 1,
			},
		}}
	}

	// Create XSD validator
	xsdVal := xsd.NewValidator(schema)

	// Perform validation
	violations := xsdVal.Validate(doc)

	// Convert violations to diagnostics
	converter := xsd.NewDiagnosticConverter(v.config.SourceName, source)
	xsdDiags := converter.Convert(violations)

	// Convert XSD diagnostics to our format
	var diagnostics []Diagnostic
	for _, xd := range xsdDiags {
		// Skip E205 errors for special SCXML target constants
		// These are runtime constants like #_parent, #_internal, etc.
		if xd.Code == "E205" {
			// Extract the target ID from the message
			targetID := extractTargetFromE205Message(xd.Message)
			if targetID != "" && isSpecialTarget(targetID) {
				// Skip this diagnostic - it's a false positive for special targets
				continue
			}
		}

		diag := Diagnostic{
			Severity:  Severity(xd.Severity),
			Code:      xd.Code,
			Message:   xd.Message,
			Position:  Position(xd.Position),
			Tag:       xd.Tag,
			Attribute: xd.Attribute,
			SpecRef:   xd.SpecRef,
			Hints:     xd.Hints,
		}

		// Convert related positions
		for _, r := range xd.Related {
			diag.Related = append(diag.Related, Related{
				Label:    r.Label,
				Position: Position(r.Position),
			})
		}

		diagnostics = append(diagnostics, diag)
	}

	// Add ID/IDREF constraint validation
	// XSD structural validation doesn't check ID/IDREF constraints
	idrefDiags := v.validateIDREFConstraints(doc)
	diagnostics = append(diagnostics, idrefDiags...)

	// Load and validate JSON schemas from schema:* attributes
	jsonSchemaDiags := v.loadAndValidateJSONSchemas(doc, baseDir)
	diagnostics = append(diagnostics, jsonSchemaDiags...)

	return diagnostics
}

// validateIDREFConstraints validates ID/IDREF constraints
// The XSD validator handles structural validation but doesn't check ID/IDREF constraints
func (v *xsdValidator) validateIDREFConstraints(doc xmldom.Document) []Diagnostic {
	var diagnostics []Diagnostic

	root := doc.DocumentElement()
	if root == nil {
		return diagnostics
	}

	// Collect all IDs and check for duplicates
	idMap := make(map[string]xmldom.Element)
	var elements []xmldom.Element

	// Recursively collect elements
	var collect func(xmldom.Element)
	collect = func(e xmldom.Element) {
		elements = append(elements, e)
		id := string(e.GetAttribute("id"))
		if id != "" {
			if existing, found := idMap[id]; found {
				// Duplicate ID
				line, col, off := e.Position()
				existLine, existCol, existOff := existing.Position()
				diagnostics = append(diagnostics, Diagnostic{
					Severity: SeverityError,
					Code:     "E206",
					Message:  "Duplicate ID value '" + id + "'",
					Position: Position{
						File:   v.config.SourceName,
						Line:   line,
						Column: col,
						Offset: off,
					},
					Tag:       string(e.LocalName()),
					Attribute: "id",
					Hints: []string{
						"Each ID must be unique within the document",
						"IDs are used to reference elements (e.g., in transition targets)",
					},
					Related: []Related{
						{
							Label: "first defined here",
							Position: Position{
								File:   v.config.SourceName,
								Line:   existLine,
								Column: existCol,
								Offset: existOff,
							},
						},
					},
				})
			} else {
				idMap[id] = e
			}
		}

		// Recurse into children
		children := e.Children()
		for i := uint(0); i < children.Length(); i++ {
			if child := children.Item(i); child != nil {
				collect(child)
			}
		}
	}
	collect(root)

	// Check IDREF/IDREFS attributes
	for _, elem := range elements {
		tagName := string(elem.LocalName())

		// Check transition target (IDREFS)
		if tagName == "transition" {
			if target := string(elem.GetAttribute("target")); target != "" {
				// target can be multiple space-separated IDs
				targets := splitIDREFS(target)
				for _, t := range targets {
					// Skip special constant targets that are part of SCXML spec
					// These don't need to reference actual IDs in the document
					if isSpecialTarget(t) {
						continue
					}

					if _, found := idMap[t]; !found {
						line, col, off := elem.Position()
						diagnostics = append(diagnostics, Diagnostic{
							Severity: SeverityError,
							Code:     "E205",
							Message:  "Referenced ID '" + t + "' does not exist in document",
							Position: Position{
								File:   v.config.SourceName,
								Line:   line,
								Column: col,
								Offset: off,
							},
							Tag:       tagName,
							Attribute: "target",
							Hints: []string{
								"Ensure there is an element with id='" + t + "' in the document",
								"Check for typos in the ID reference",
								"IDs are case-sensitive",
							},
						})
					}
				}
			}
		}

		// Check initial attribute (IDREFS for scxml and state elements)
		if tagName == "scxml" || tagName == "state" {
			if initial := string(elem.GetAttribute("initial")); initial != "" {
				// initial can be multiple space-separated IDs
				initials := splitIDREFS(initial)
				for _, init := range initials {
					if _, found := idMap[init]; !found {
						line, col, off := elem.Position()
						diagnostics = append(diagnostics, Diagnostic{
							Severity: SeverityError,
							Code:     "E205",
							Message:  "Referenced ID '" + init + "' does not exist in document",
							Position: Position{
								File:   v.config.SourceName,
								Line:   line,
								Column: col,
								Offset: off,
							},
							Tag:       tagName,
							Attribute: "initial",
							Hints: []string{
								"Ensure there is a state with id='" + init + "' in the document",
								"The initial attribute should reference a valid state ID",
								"IDs are case-sensitive",
							},
						})
					}
				}
			}
		}
	}

	return diagnostics
}

// extractTargetFromE205Message extracts the target ID from an E205 error message
// Example: "Referenced ID '#_parent' does not exist in document" → "#_parent"
func extractTargetFromE205Message(msg string) string {
	// Look for text between single quotes
	start := -1
	for i, ch := range msg {
		if ch == '\'' {
			if start == -1 {
				start = i + 1
			} else {
				return msg[start:i]
			}
		}
	}
	return ""
}

// isSpecialTarget checks if a target is a special SCXML constant that doesn't need ID validation
// Special targets include:
// - #_internal: internal transitions
// - #_parent: send to parent session
// - #_scxml_*: SCXML session IDs
// - #_invoke* or #_*: invoke IDs
func isSpecialTarget(target string) bool {
	// All special targets start with #_
	// These are runtime constants defined by the SCXML spec
	return len(target) > 0 && target[0] == '#' && len(target) > 1 && target[1] == '_'
}

// splitIDREFS splits a space-separated list of IDs (IDREFS type)
func splitIDREFS(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, id := range splitByWhitespace(s) {
		if id != "" {
			result = append(result, id)
		}
	}
	return result
}

// splitByWhitespace splits a string by any whitespace
func splitByWhitespace(s string) []string {
	var result []string
	var current []rune
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if len(current) > 0 {
				result = append(result, string(current))
				current = nil
			}
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	return result
}

// loadAndValidateJSONSchemas loads JSON schemas from schema:* attributes and validates data/transition elements
func (v *xsdValidator) loadAndValidateJSONSchemas(doc xmldom.Document, baseDir string) []Diagnostic {
	var diagnostics []Diagnostic

	root := doc.DocumentElement()
	if root == nil {
		return diagnostics
	}

	// Extract schema:* declarations from root element
	declarations, err := ExtractSchemaDeclarations(root)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: SeverityError,
			Code:     "E_SCHEMA_DECL",
			Message:  fmt.Sprintf("Failed to extract schema declarations: %v", err),
			Position: Position{
				File: v.config.SourceName,
				Line: 1,
			},
		})
		return diagnostics
	}

	if len(declarations) == 0 {
		// No schema declarations, skip JSON schema validation
		return diagnostics
	}

	slog.Debug("[JSON_SCHEMA] Found schema declarations", "count", len(declarations))

	// Load all declared schemas
	schemas, err := LoadDeclaredSchemas(declarations, baseDir)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: SeverityError,
			Code:     "E_SCHEMA_LOAD",
			Message:  fmt.Sprintf("Failed to load schemas: %v", err),
			Position: Position{
				File: v.config.SourceName,
				Line: 1,
			},
		})
		return diagnostics
	}

	// Store loaded schemas in config for runtime use
	v.config.JSONSchemas = schemas
	slog.Debug("[JSON_SCHEMA] Loaded schemas", "count", len(schemas))

	// Validate data elements with schema attributes
	dataDiags := v.validateDataSchemas(doc, schemas)
	diagnostics = append(diagnostics, dataDiags...)

	// Validate transition elements with schema attributes
	transitionDiags := v.validateTransitionSchemas(doc, schemas)
	diagnostics = append(diagnostics, transitionDiags...)

	return diagnostics
}

// validateDataSchemas validates <data> elements that have schema attributes
func (v *xsdValidator) validateDataSchemas(doc xmldom.Document, schemas map[string]*jsonschema.Schema) []Diagnostic {
	var diagnostics []Diagnostic

	// Find all data elements
	dataElements := v.findElementsByTagName(doc, "data")

	for _, dataElem := range dataElements {
		schemaAttr := string(dataElem.GetAttribute("schema"))
		if schemaAttr == "" {
			continue // No schema validation needed
		}

		// Parse schema reference
		ref, err := ParseSchemaReference(schemaAttr)
		if err != nil {
			line, col, off := dataElem.Position()
			diagnostics = append(diagnostics, Diagnostic{
				Severity:  SeverityError,
				Code:      "E_SCHEMA_REF",
				Message:   fmt.Sprintf("Invalid schema reference: %v", err),
				Position:  Position{File: v.config.SourceName, Line: line, Column: col, Offset: off},
				Tag:       "data",
				Attribute: "schema",
			})
			continue
		}

		// Resolve schema
		schema, err := ResolveSchemaReference(ref, schemas)
		if err != nil {
			line, col, off := dataElem.Position()
			diagnostics = append(diagnostics, Diagnostic{
				Severity:  SeverityError,
				Code:      "E_SCHEMA_RESOLVE",
				Message:   fmt.Sprintf("Failed to resolve schema: %v", err),
				Position:  Position{File: v.config.SourceName, Line: line, Column: col, Offset: off},
				Tag:       "data",
				Attribute: "schema",
			})
			continue
		}

		// If data element has expr attribute, validate it
		exprAttr := string(dataElem.GetAttribute("expr"))
		if exprAttr != "" {
			// Try to parse expr as JSON
			var exprData interface{}
			if err := json.Unmarshal([]byte(exprAttr), &exprData); err != nil {
				// If not valid JSON, skip validation (might be evaluated expression)
				line, col, off := dataElem.Position()
				diagnostics = append(diagnostics, Diagnostic{
					Severity:  SeverityInfo,
					Code:      "I_SCHEMA_SKIP",
					Message:   "Data expr is not JSON literal, schema validation skipped (will be validated at runtime)",
					Position:  Position{File: v.config.SourceName, Line: line, Column: col, Offset: off},
					Tag:       "data",
					Attribute: "expr",
				})
				continue
			}

			// Validate against schema
			validationResult := jsonschema.ValidateDocument(exprData, schema)
			if !validationResult.Valid {
				line, col, off := dataElem.Position()
				// Collect error messages
				var errorMsgs []string
				for _, verr := range validationResult.Errors {
					errorMsgs = append(errorMsgs, verr.Message)
				}
				diagnostics = append(diagnostics, Diagnostic{
					Severity:  SeverityWarning, // Warning since expr might be evaluated later
					Code:      "W_SCHEMA_VALID",
					Message:   fmt.Sprintf("Data expr does not match schema: %v", errorMsgs),
					Position:  Position{File: v.config.SourceName, Line: line, Column: col, Offset: off},
					Tag:       "data",
					Attribute: "expr",
					Hints:     []string{"Ensure the expr value matches the declared schema", "This will be validated at runtime as well"},
				})
			}
		}
	}

	return diagnostics
}

// validateTransitionSchemas validates <transition> elements that have schema attributes
func (v *xsdValidator) validateTransitionSchemas(doc xmldom.Document, schemas map[string]*jsonschema.Schema) []Diagnostic {
	var diagnostics []Diagnostic

	// Find all transition elements
	transitionElements := v.findElementsByTagName(doc, "transition")

	for _, transElem := range transitionElements {
		schemaAttr := string(transElem.GetAttribute("schema"))
		if schemaAttr == "" {
			continue // No schema validation needed
		}

		// Parse schema reference
		ref, err := ParseSchemaReference(schemaAttr)
		if err != nil {
			line, col, off := transElem.Position()
			diagnostics = append(diagnostics, Diagnostic{
				Severity:  SeverityError,
				Code:      "E_SCHEMA_REF",
				Message:   fmt.Sprintf("Invalid schema reference: %v", err),
				Position:  Position{File: v.config.SourceName, Line: line, Column: col, Offset: off},
				Tag:       "transition",
				Attribute: "schema",
			})
			continue
		}

		// Resolve schema to ensure it exists
		_, err = ResolveSchemaReference(ref, schemas)
		if err != nil {
			line, col, off := transElem.Position()
			diagnostics = append(diagnostics, Diagnostic{
				Severity:  SeverityError,
				Code:      "E_SCHEMA_RESOLVE",
				Message:   fmt.Sprintf("Failed to resolve schema: %v", err),
				Position:  Position{File: v.config.SourceName, Line: line, Column: col, Offset: off},
				Tag:       "transition",
				Attribute: "schema",
			})
			continue
		}

		// Schema reference is valid - actual validation happens at runtime when events are processed
	}

	return diagnostics
}

// findElementsByTagName finds all elements with a given tag name
func (v *xsdValidator) findElementsByTagName(doc xmldom.Document, tagName string) []xmldom.Element {
	var elements []xmldom.Element
	var collect func(xmldom.Element)
	collect = func(e xmldom.Element) {
		if string(e.LocalName()) == tagName {
			elements = append(elements, e)
		}
		children := e.Children()
		for i := uint(0); i < children.Length(); i++ {
			if child := children.Item(i); child != nil {
				collect(child)
			}
		}
	}

	root := doc.DocumentElement()
	if root != nil {
		collect(root)
	}

	return elements
}
