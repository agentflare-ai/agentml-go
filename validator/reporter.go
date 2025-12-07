package validator

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// PrettyReporter renders human-friendly diagnostics with code frames
// Inspired by rustc-style output

type PrettyReporter struct {
	w     io.Writer
	color bool

	contextBefore   int
	contextAfter    int
	showFullElement bool
	maxElementLines int // 0 = unlimited

	lastDiagAttribute string // transient: attribute name being reported (for caret)
}

// PrettyConfig configures PrettyReporter construction
// Zero values are treated as sensible defaults (e.g., 1 line of context)
type PrettyConfig struct {
	Color           bool
	ContextBefore   int
	ContextAfter    int
	ShowFullElement bool
	MaxElementLines int // 0 = unlimited
}

func NewPrettyReporter(w io.Writer, maybeCfg ...PrettyConfig) *PrettyReporter {
	// Default config if none provided
	cfg := PrettyConfig{
		Color:           false,
		ContextBefore:   1,
		ContextAfter:    1,
		ShowFullElement: true,
		MaxElementLines: 0,
	}
	// If caller provided a config, use the last one (pattern: maybeConfig ...)
	for _, c := range maybeCfg {
		cfg = c
	}
	return &PrettyReporter{
		w:               w,
		color:           cfg.Color,
		contextBefore:   cfg.ContextBefore,
		contextAfter:    cfg.ContextAfter,
		showFullElement: cfg.ShowFullElement,
		maxElementLines: cfg.MaxElementLines,
	}
}

func (r *PrettyReporter) Print(sourceName string, source string, diags []Diagnostic) error {
	if len(diags) == 0 {
		fmt.Fprintf(r.w, "%s: ok (no issues)\n", nonEmpty(sourceName, "<input>"))
		return nil
	}

	srcIdx := buildSourceIndex(source)

	for _, d := range SortedDiagnostics(diags) {
		file := nonEmpty(d.Position.File, sourceName)
		// hold attribute for caret computation within this frame
		r.lastDiagAttribute = d.Attribute
		loc := locationString(file, d.Position.Line, d.Position.Column)
		head := fmt.Sprintf("%s: %s[%s] %s", loc, strings.ToUpper(string(d.Severity)), d.Code, d.Message)
		fmt.Fprintln(r.w, r.styleHeader(head, d.Severity))
		// Code frame (only if we have a line number)
		if d.Position.Line > 0 {
			r.printFrame(srcIdx, d.Position.Line, d.Position.Column, d.Tag)
		}
		// Hints
		for _, h := range d.Hints {
			fmt.Fprintln(r.w, r.styleHint("  hint: "+h))
		}
		// Related with inline frames
		for _, rel := range d.Related {
			rloc := locationString(nonEmpty(rel.Position.File, file), rel.Position.Line, rel.Position.Column)
			fmt.Fprintf(r.w, "  note: %s (%s)\n", rel.Label, rloc)
			if rel.Position.Line > 0 {
				// single-line frame
				if line, ok := srcIdx.line(rel.Position.Line); ok {
					prefix := fmt.Sprintf("  %6d | ", rel.Position.Line)
					fmt.Fprintf(r.w, "%s%s\n", prefix, trimRight(line))
					indent := visualIndent(line, rel.Position.Column, 8)
					fmt.Fprintf(r.w, "%s%s%s\n", strings.Repeat(" ", len(prefix)), strings.Repeat(" ", indent), r.styleCaret("^"))
				}
			}
		}
		fmt.Fprintln(r.w)
	}
	// Summary
	total, errs, warns := len(diags), 0, 0
	for _, d := range diags {
		if d.Severity == SeverityError {
			errs++
		}
		if d.Severity == SeverityWarning {
			warns++
		}
	}
	fmt.Fprintf(r.w, "summary: %d error(s), %d warning(s), %d total\n", errs, warns, total)
	return nil
}

func (r *PrettyReporter) printFrame(src *sourceIndex, line, col int, tag string) {
	if line <= 0 {
		return
	}

	// Determine caret position strictly from diagnostic position (rely on xmldom node positions)
	caretLine, caretCol := line, col

	// Expand to full element block if requested and tag is available
	start, end := caretLine-r.contextBefore, caretLine+r.contextAfter
	if start < 1 {
		start = 1
	}
	if r.showFullElement && tag != "" {
		open := findTagOpenLine(src, caretLine, tag)
		close := findElementCloseLine(src, open, tag, r.maxElementLines)
		start, end = open, close
		if r.maxElementLines > 0 && end-start+1 > r.maxElementLines {
			end = start + r.maxElementLines - 1
		}
	}

	// Print frame lines
	for ln := start; ln <= end; ln++ {
		text, ok := src.line(ln)
		if !ok {
			break
		}
		prefix := fmt.Sprintf("  %6d | ", ln)
		fmt.Fprintf(r.w, "%s%s\n", prefix, trimRight(text))
		if ln == caretLine {
			c := caretCol
			// If we know the attribute name and the caret is at the start of the attribute,
			// advance to the opening quote of its value (common expectation for attribute errors)
			var attrStartCol, attrLen int
			if r.lastDiagAttribute != "" && c > 0 {
				if adj := adjustToAttributeQuote(text, r.lastDiagAttribute, c); adj > 0 {
					c = adj
					// attempt to compute attribute value length for underline
					attrStartCol, attrLen = findAttrValueSpanOnLine(text, r.lastDiagAttribute)
				}
			}
			if c <= 0 {
				c = firstNonSpace(text) + 1
			}
			if c <= 1 {
				c = 1
			}
			// Compute visual indent accounting for tabs and runes (tab width = 8)
			indent := visualIndent(text, c, 8)
			underline := r.styleCaret("^")
			if attrStartCol > 0 && attrLen > 0 {
				// build ^~~~ underline matching attribute value length
				// shift indent to attribute start if different
				indent = visualIndent(text, attrStartCol, 8)
				underline = r.styleCaret("^" + strings.Repeat("~", max(0, attrLen-1)))
			}
			fmt.Fprintf(r.w, "%s%s%s\n", strings.Repeat(" ", len(prefix)), strings.Repeat(" ", indent), underline)
		}
	}
}

// findElementCloseLine scans forward from startLine to find
// either a self-closing "/>" or the matching closing tag "</tag".
// Returns at most startLine+maxSpan-1 if maxSpan>0.
func findElementCloseLine(src *sourceIndex, startLine int, tag string, maxSpan int) int {
	limit := len(src.lines) - 1
	if maxSpan > 0 {
		if startLine+maxSpan-1 < limit {
			limit = startLine + maxSpan - 1
		}
	}
	closing := "</" + tag
	for ln := startLine; ln <= limit; ln++ {
		line, ok := src.line(ln)
		if !ok {
			break
		}
		if strings.Contains(line, "/>") {
			return ln
		}
		if strings.Contains(line, closing) {
			return ln
		}
	}
	return startLine
}

// JSONReporter emits JSON with diagnostics

type JSONReporter struct {
	w io.Writer
}

type JSONConfig struct{}

func NewJSONReporter(w io.Writer, _ ...JSONConfig) *JSONReporter { return &JSONReporter{w: w} }

func (r *JSONReporter) Print(result Result) error {
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// --- helpers / styling ---

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func locationString(file string, line, col int) string {
	if file == "" && line == 0 {
		return "<input>:0:0"
	}
	return fmt.Sprintf("%s:%d:%d", nonEmpty(file, "<input>"), line, col)
}

func trimRight(s string) string {
	return strings.TrimRight(s, "\t \r\n")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func firstNonSpace(s string) int {
	for i, r := range s {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return 0
}

// bestCaretColumn tries to compute a caret position relative to attribute value or tag name
// If an attribute name is provided and present on the line, point to the beginning of its value.
// Else, if the tag appears on the line (e.g., <transition), point to the '<' of that tag.
// Else, fall back to the provided column.
func bestCaretColumn(line string, fallbackCol int, tag string, attr string) int {
	// Try attribute value
	if attr != "" {
		if c := findAttrValueColumn(line, attr); c > 0 {
			return c
		}
	}
	// Try tag anchor
	if tag != "" {
		if c := findTagColumn(line, tag); c > 0 {
			return c
		}
	}
	return fallbackCol
}

// findAttrValueColumn finds the 1-based column of the first character inside the attribute's quoted value
func findAttrValueColumn(line, attr string) int {
	// find attr name
	idx := strings.Index(line, attr)
	if idx < 0 {
		return 0
	}
	// find '=' after attr
	eq := strings.Index(line[idx+len(attr):], "=")
	if eq < 0 {
		return 0
	}
	eqPos := idx + len(attr) + eq + 1
	// skip optional spaces
	i := eqPos + 1
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) {
		return 0
	}
	// expect quote
	quote := line[i]
	if quote != '"' && quote != '\'' {
		return 0
	}
	// value starts at next character
	start := i + 1
	// return 1-based column
	return start + 1
}

// findTagColumn finds the 1-based column of the '<' of the opening tag
func findTagColumn(line, tag string) int {
	needle := "<" + tag
	idx := strings.Index(line, needle)
	if idx < 0 {
		return 0
	}
	return idx + 1
}

func findAttrValueSpanOnLine(line, attr string) (startCol int, length int) {
	idx := strings.Index(line, attr)
	if idx < 0 {
		return 0, 0
	}
	j := idx + len(attr)
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	if j >= len(line) || line[j] != '=' {
		return 0, 0
	}
	j++
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	if j >= len(line) {
		return 0, 0
	}
	quote := line[j]
	if quote != '"' && quote != '\'' {
		return 0, 0
	}
	valStart := j + 1
	k := valStart
	for k < len(line) && line[k] != quote {
		k++
	}
	if k >= len(line) {
		return 0, 0
	}
	// return 1-based column for first char of value, and rune length (approx by bytes here)
	return valStart + 1, max(0, k-valStart)
}

// The following helpers remain for potential future use when positions
// are unavailable or imprecise, but are not used when diagnostics include
// accurate node/attribute positions.

// findTagOpenLine searches backward and forward to locate the line with the opening tag
func findTagOpenLine(src *sourceIndex, approxLine int, tag string) int {
	if tag == "" {
		return approxLine
	}
	// search backward up to 50 lines
	low := approxLine - 50
	if low < 1 {
		low = 1
	}
	for ln := approxLine; ln >= low; ln-- {
		if s, ok := src.line(ln); ok {
			if findTagColumn(s, tag) > 0 {
				return ln
			}
		}
	}
	// search forward up to 50 lines
	high := approxLine + 50
	max := len(src.lines) - 1
	if high > max {
		high = max
	}
	for ln := approxLine + 1; ln <= high; ln++ {
		if s, ok := src.line(ln); ok {
			if findTagColumn(s, tag) > 0 {
				return ln
			}
		}
	}
	return approxLine
}

// computeCaretPosition finds the most accurate caret position in the element block
func computeCaretPosition(src *sourceIndex, openLine, endLine, fallbackLine, fallbackCol int, tag, attr string) (int, int) {
	// If attribute provided, prefer caret at the start of the attribute name within the start tag
	if attr != "" {
		startEnd := findStartTagEndLine(src, openLine, endLine)
		if startEnd == 0 {
			startEnd = endLine
		}
		if ln, col := findAttrNameAcrossLines(src, openLine, startEnd, attr); ln > 0 && col > 0 {
			return ln, col
		}
		// Fallback: caret at start of attribute value if name not found cleanly
		if vln, vcol := findAttrValueAcrossLines(src, openLine, endLine, attr); vln > 0 && vcol > 0 {
			return vln, vcol
		}
		// If we still didn't find it, use fallback if precise
		if fallbackLine > 0 && fallbackCol > 0 {
			return fallbackLine, fallbackCol
		}
	}
	// Else anchor to tag start on opening line if available
	if tag != "" {
		if openLine > 0 {
			if s, ok := src.line(openLine); ok {
				if c := findTagColumn(s, tag); c > 0 {
					return openLine, c
				}
			}
		}
	}
	return fallbackLine, fallbackCol
}

// findAttrNameAcrossLines searches [openLine,startEndLine] for the attribute name and returns its starting column (1-based)
func findAttrNameAcrossLines(src *sourceIndex, openLine, startEndLine int, attr string) (int, int) {
	for ln := openLine; ln <= startEndLine; ln++ {
		line, ok := src.line(ln)
		if !ok {
			break
		}
		idx := strings.Index(line, attr)
		if idx < 0 {
			continue
		}
		// Heuristic boundary checks: preceding char is whitespace or '<' or start of line
		if idx > 0 {
			prev := line[idx-1]
			if !(prev == ' ' || prev == '\t' || prev == '<') {
				continue
			}
		}
		// Following char is '=' or whitespace
		if idx+len(attr) < len(line) {
			next := line[idx+len(attr)]
			if !(next == '=' || next == ' ' || next == '\t') {
				continue
			}
		}
		return ln, idx + 1 // 1-based column
	}
	return 0, 0
}

// findAttrValueAcrossLines searches [openLine,endLine] for attr name and returns the column of the first character inside its quoted value
func findAttrValueAcrossLines(src *sourceIndex, openLine, endLine int, attr string) (int, int) {
	for ln := openLine; ln <= endLine; ln++ {
		line, ok := src.line(ln)
		if !ok {
			break
		}
		idx := strings.Index(line, attr)
		if idx < 0 {
			continue
		}
		// found attribute name; search for '=' possibly across lines
		curLine := ln
		curIdx := idx + len(attr)
		for {
			rest := ""
			if s, ok := src.line(curLine); ok {
				rest = s[curIdx:]
			}
			eqPos := strings.Index(rest, "=")
			if eqPos >= 0 {
				// position after '=' in current line
				eqAbsCol := curIdx + eqPos + 1 // 0-based
				// scan for first quote after '=' possibly across lines
				qLine, qCol := findQuoteAfter(src, curLine, eqAbsCol)
				if qLine > 0 && qCol > 0 {
					// caret is at first character after the quote
					return qLine, qCol + 1
				}
				return 0, 0
			}
			// move to next line and continue
			curLine++
			if curLine > endLine {
				break
			}
			curIdx = 0
		}
	}
	return 0, 0
}

// findStartTagEndLine scans from openLine to endLine for the first '>' that closes the start tag
func findStartTagEndLine(src *sourceIndex, openLine, endLine int) int {
	for ln := openLine; ln <= endLine; ln++ {
		line, ok := src.line(ln)
		if !ok {
			break
		}
		if strings.Contains(line, ">") {
			return ln
		}
	}
	return 0
}

// adjustToAttributeQuote advances from the start of an attribute name to the opening quote of its value
// Column is 1-based
func adjustToAttributeQuote(line, attr string, col int) int {
	if col <= 0 {
		return 0
	}
	idx := col - 1
	// Verify we're at the attribute name; if not, try to locate the attribute name in the line
	if idx < 0 || idx+len(attr) > len(line) || line[idx:idx+len(attr)] != attr {
		pos := strings.Index(line, attr)
		if pos < 0 {
			return 0
		}
		idx = pos
	}
	j := idx + len(attr)
	// Skip spaces
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	// Expect '=' possibly later on the same line
	if j >= len(line) || line[j] != '=' {
		pos := strings.Index(line[j:], "=")
		if pos < 0 {
			return 0
		}
		j = j + pos
	}
	// Move past '='
	j++
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	if j >= len(line) {
		return 0
	}
	// At this point j should be at the opening quote
	if line[j] != '"' && line[j] != '\'' {
		// search ahead on this line for a quote
		pos := strings.IndexAny(line[j:], "'\"")
		if pos < 0 {
			return 0
		}
		j = j + pos
	}
	return j + 1 // convert to 1-based column
}

// visualIndent computes the number of spaces needed to visually reach column 'col' (1-based)
// Expands tabs using tabWidth (commonly 8). Treats runes as width 1 for simplicity.
func visualIndent(line string, col int, tabWidth int) int {
	if col <= 1 {
		return 0
	}
	// Walk runes, count visible columns until reaching position col-1
	visible := 0
	pos := 1 // 1-based rune index
	for _, r := range line {
		if pos >= col {
			break
		}
		if r == '\t' {
			// advance to next tab stop
			next := tabWidth - (visible % tabWidth)
			if next == 0 {
				next = tabWidth
			}
			visible += next
		} else {
			visible += 1
		}
		pos++
	}
	return visible
}

// findQuoteAfter looks for the first ' or " after the provided 0-based column on the given line, scanning forward across lines
func findQuoteAfter(src *sourceIndex, line, col0 int) (int, int) {
	// same line first
	if s, ok := src.line(line); ok {
		if col0 < len(s) {
			for i := col0 + 1; i < len(s); i++ {
				if s[i] == '"' || s[i] == '\'' {
					return line, i
				}
			}
		}
	}
	// scan subsequent lines
	max := len(src.lines) - 1
	for ln := line + 1; ln <= max; ln++ {
		if s, ok := src.line(ln); ok {
			for i := 0; i < len(s); i++ {
				if s[i] == '"' || s[i] == '\'' {
					return ln, i
				}
			}
		}
	}
	return 0, 0
}

func (r *PrettyReporter) styleHeader(s string, sev Severity) string {
	if !r.color {
		return s
	}
	switch sev {
	case SeverityError:
		return "\x1b[31m" + s + "\x1b[0m" // red
	case SeverityWarning:
		return "\x1b[33m" + s + "\x1b[0m" // yellow
	default:
		return s
	}
}

func (r *PrettyReporter) styleHint(s string) string {
	if !r.color {
		return s
	}
	return "\x1b[36m" + s + "\x1b[0m" // cyan
}

func (r *PrettyReporter) styleCaret(s string) string {
	if !r.color {
		return s
	}
	return "\x1b[31m" + s + "\x1b[0m"
}

// sourceIndex helps pretty reporter render code frames quickly
// Build once per source and reuse
type sourceIndex struct {
	lines []string // 1-based in usage (lines[0] unused)
}

func buildSourceIndex(src string) *sourceIndex {
	if src == "" {
		return &sourceIndex{lines: []string{""}}
	}
	// Normalize newlines to \n for consistent reporting
	norm := strings.ReplaceAll(src, "\r\n", "\n")
	norm = strings.ReplaceAll(norm, "\r", "\n")
	parts := strings.Split(norm, "\n")
	// Prepend a dummy so we can index using 1-based line numbers
	lines := make([]string, 1, len(parts)+1)
	lines = append(lines, parts...)
	return &sourceIndex{lines: lines}
}

func (s *sourceIndex) line(n int) (string, bool) {
	if n <= 0 || n >= len(s.lines) {
		return "", false
	}
	return s.lines[n], true
}

// SortedDiagnostics returns diagnostics sorted by file/line/column
func SortedDiagnostics(diags []Diagnostic) []Diagnostic {
	out := append([]Diagnostic(nil), diags...)
	for i := 0; i < len(out)-1; i++ {
		for j := i + 1; j < len(out); j++ {
			if shouldSwap(out[i], out[j]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func shouldSwap(a, b Diagnostic) bool {
	if a.Position.File != b.Position.File {
		return a.Position.File > b.Position.File
	}
	if a.Position.Line != b.Position.Line {
		return a.Position.Line > b.Position.Line
	}
	if a.Position.Column != b.Position.Column {
		return a.Position.Column > b.Position.Column
	}
	return a.Code > b.Code
}
