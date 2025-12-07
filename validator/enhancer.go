package validator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/agentflare-ai/go-xmldom"
)

// enhanceDiagnostics post-processes XSD diagnostics to add fuzzy matching and helpful hints
func enhanceDiagnostics(doc xmldom.Document, diagnostics []Diagnostic) []Diagnostic {
	if doc == nil || len(diagnostics) == 0 {
		return diagnostics
	}

	root := doc.DocumentElement()
	if root == nil {
		return diagnostics
	}

	// Collect context information from document
	ctx := &enhancementContext{
		allElements: collectElements(root),
		idToElement: make(map[string]xmldom.Element),
		stateIDs:    make(map[string]struct{}),
	}

	// Build ID and state ID maps
	for _, el := range ctx.allElements {
		id := string(el.GetAttribute("id"))
		if id != "" {
			ctx.idToElement[id] = el
		}

		tag := strings.ToLower(string(el.TagName()))
		if tag == "state" || tag == "parallel" || tag == "final" || tag == "history" {
			if id != "" {
				ctx.stateIDs[id] = struct{}{}
			}
		}
	}

	// Enhance each diagnostic
	enhanced := make([]Diagnostic, 0, len(diagnostics))
	for _, diag := range diagnostics {
		enhanced = append(enhanced, ctx.enhance(diag))
	}

	return enhanced
}

// enhancementContext holds document information for enhancement
type enhancementContext struct {
	allElements []xmldom.Element
	idToElement map[string]xmldom.Element
	stateIDs    map[string]struct{}
}

// enhance adds fuzzy matching and context-aware hints to a single diagnostic
func (ctx *enhancementContext) enhance(diag Diagnostic) Diagnostic {
	switch diag.Code {
	case "E205": // cvc-id.1 - IDREF not found
		// Special handling for transition targets and initial attributes
		// These should fuzzy match against state IDs only
		if diag.Tag == "transition" && diag.Attribute == "target" {
			return ctx.enhanceTransitionTargetError(diag)
		}
		if (diag.Tag == "scxml" || diag.Tag == "state") && diag.Attribute == "initial" {
			return ctx.enhanceInitialAttributeError(diag)
		}
		// Generic IDREF error - match against all IDs
		return ctx.enhanceIDREFError(diag)

	case "E206": // cvc-id.2 - Duplicate ID
		return ctx.enhanceDuplicateIDError(diag)

	case "E200": // cvc-complex-type.3.2.2 - Invalid attribute
		return ctx.enhanceInvalidAttributeError(diag)
	}

	return diag
}

// enhanceIDREFError adds fuzzy suggestions for IDREF errors
func (ctx *enhancementContext) enhanceIDREFError(diag Diagnostic) Diagnostic {
	// Extract the missing ID from the message
	// Example: "Referenced ID 'acitve' does not exist" or "There is no ID/IDREF binding for IDREF 'acitve'"
	missingID := extractQuotedValue(diag.Message)

	if missingID != "" && len(ctx.idToElement) > 0 {
		// Find similar IDs using fuzzy matching
		suggestions := nearestIDs(missingID, ctx.idToElement, 3, 2)
		if len(suggestions) > 0 {
			// Add fuzzy suggestions to hints
			if len(suggestions) == 1 {
				diag.Hints = append(diag.Hints, fmt.Sprintf("Did you mean %q?", suggestions[0]))
			} else {
				diag.Hints = append(diag.Hints, fmt.Sprintf("Did you mean one of: %s?", quoteJoin(suggestions)))
			}
		}
	}

	return diag
}

// enhanceDuplicateIDError adds "first defined here" related position
func (ctx *enhancementContext) enhanceDuplicateIDError(diag Diagnostic) Diagnostic {
	// For duplicate IDs, try to find the first occurrence
	// Example: "Duplicate ID value 'state1'"
	duplicateID := extractQuotedValue(diag.Message)

	if duplicateID != "" {
		// Find all elements with this ID
		var occurrences []xmldom.Element
		for _, el := range ctx.allElements {
			if string(el.GetAttribute("id")) == duplicateID {
				occurrences = append(occurrences, el)
			}
		}

		// If we found multiple occurrences, add "first defined here" related position
		if len(occurrences) >= 2 {
			first := occurrences[0]
			line, col, off := first.Position()
			diag.Related = append(diag.Related, Related{
				Label: "first defined here",
				Position: Position{
					File:   diag.Position.File,
					Line:   line,
					Column: col,
					Offset: off,
				},
			})
		}
	}

	return diag
}

// enhanceInvalidAttributeError adds SCXML-specific attribute suggestions
func (ctx *enhancementContext) enhanceInvalidAttributeError(diag Diagnostic) Diagnostic {
	// XSD already provides good hints, but we can add SCXML-specific ones
	switch diag.Attribute {
	case "sendid":
		// Common mistake: using sendid on <send> instead of id
		if diag.Tag == "send" {
			// Hint likely already added by XSD converter, ensure it's there
			hasHint := false
			for _, hint := range diag.Hints {
				if strings.Contains(hint, "id") {
					hasHint = true
					break
				}
			}
			if !hasHint {
				diag.Hints = append([]string{"The <send> element uses 'id' attribute, not 'sendid'"}, diag.Hints...)
			}
		}

	case "priority":
		// Common mistake: trying to add priority to transitions
		if diag.Tag == "transition" {
			hasHint := false
			for _, hint := range diag.Hints {
				if strings.Contains(hint, "priority") {
					hasHint = true
					break
				}
			}
			if !hasHint {
				diag.Hints = append([]string{
					"The 'priority' attribute is not part of standard SCXML",
					"Transition selection is based on document order",
				}, diag.Hints...)
			}
		}
	}

	return diag
}

// enhanceTransitionTargetError adds fuzzy suggestions for transition targets
func (ctx *enhancementContext) enhanceTransitionTargetError(diag Diagnostic) Diagnostic {
	// Extract target ID from message
	// Example: "transition target 'acitve' not found"
	targetID := extractQuotedValue(diag.Message)

	if targetID != "" && len(ctx.stateIDs) > 0 {
		// Find similar state IDs using fuzzy matching
		suggestions := nearestIDs(targetID, ctx.stateIDs, 3, 2)
		if len(suggestions) > 0 {
			if len(suggestions) == 1 {
				diag.Hints = append(diag.Hints, fmt.Sprintf("Did you mean %q?", suggestions[0]))
			} else {
				diag.Hints = append(diag.Hints, fmt.Sprintf("Did you mean one of: %s?", quoteJoin(suggestions)))
			}
		}
	}

	return diag
}

// enhanceInitialAttributeError adds fuzzy suggestions for initial attribute
func (ctx *enhancementContext) enhanceInitialAttributeError(diag Diagnostic) Diagnostic {
	// Extract initial state ID from message
	// Example: "initial state 'acitve' not found"
	initialID := extractQuotedValue(diag.Message)

	if initialID != "" && len(ctx.stateIDs) > 0 {
		// Find similar state IDs
		suggestions := nearestIDs(initialID, ctx.stateIDs, 3, 2)
		if len(suggestions) > 0 {
			if len(suggestions) == 1 {
				diag.Hints = append(diag.Hints, fmt.Sprintf("Did you mean %q?", suggestions[0]))
			} else {
				diag.Hints = append(diag.Hints, fmt.Sprintf("Did you mean one of: %s?", quoteJoin(suggestions)))
			}
		}
	}

	return diag
}

// --- Helper functions ---

// collectElements recursively collects all elements in document order
func collectElements(root xmldom.Element) []xmldom.Element {
	var out []xmldom.Element
	var rec func(xmldom.Element)
	rec = func(e xmldom.Element) {
		out = append(out, e)
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
	return out
}

// nearestIDs returns up to 'limit' closest ids within maxDistance (inclusive)
// Works with both map[string]xmldom.Element and map[string]struct{}
func nearestIDs[V any](miss string, set map[string]V, limit int, maxDistance int) []string {
	type cand struct {
		s string
		d int
	}
	cands := make([]cand, 0, len(set))
	for k := range set {
		d := simpleDistance(miss, k)
		if d <= maxDistance {
			cands = append(cands, cand{s: k, d: d})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].d != cands[j].d {
			return cands[i].d < cands[j].d
		}
		return cands[i].s < cands[j].s
	})
	if limit <= 0 || limit > len(cands) {
		limit = len(cands)
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, cands[i].s)
	}
	return out
}

// simpleDistance calculates Levenshtein distance between two strings
func simpleDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n, m := len(ra), len(rb)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}
	dp := make([]int, (n+1)*(m+1))
	idx := func(i, j int) int { return i*(m+1) + j }
	for i := 0; i <= n; i++ {
		dp[idx(i, 0)] = i
	}
	for j := 0; j <= m; j++ {
		dp[idx(0, j)] = j
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			ins := dp[idx(i, j-1)] + 1
			del := dp[idx(i-1, j)] + 1
			sub := dp[idx(i-1, j-1)] + cost
			v := ins
			if del < v {
				v = del
			}
			if sub < v {
				v = sub
			}
			dp[idx(i, j)] = v
		}
	}
	return dp[idx(n, m)]
}

// quoteJoin quotes and joins strings
func quoteJoin(s []string) string {
	quoted := make([]string, len(s))
	for i := range s {
		quoted[i] = fmt.Sprintf("%q", s[i])
	}
	return strings.Join(quoted, ", ")
}

// extractQuotedValue extracts the first single-quoted or double-quoted value from a string
// Example: "Referenced ID 'acitve' does not exist" → "acitve"
func extractQuotedValue(s string) string {
	// Try single quotes first
	if idx := strings.Index(s, "'"); idx != -1 {
		if end := strings.Index(s[idx+1:], "'"); end != -1 {
			return s[idx+1 : idx+1+end]
		}
	}
	// Try double quotes
	if idx := strings.Index(s, "\""); idx != -1 {
		if end := strings.Index(s[idx+1:], "\""); end != -1 {
			return s[idx+1 : idx+1+end]
		}
	}
	return ""
}
