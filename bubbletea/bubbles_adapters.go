package bubbletea

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/stopwatch"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/timer"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"go.opentelemetry.io/otel/attribute"
)

func bindComponentConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter, cfg any) error {
	eval := func(ctx context.Context, expr string) (any, error) {
		if itp == nil || itp.DataModel() == nil {
			return nil, errNoDataModel
		}
		return itp.DataModel().EvaluateValue(ctx, expr)
	}
	if err := bindAttributesWithEval(ctx, el, cfg, eval); err != nil {
		data := map[string]any{
			"element": displayName,
		}
		var attrErr *attrError
		if errors.As(err, &attrErr) {
			data["attribute"] = attrErr.Attr
			data["value"] = attrErr.Value
		}
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("%s %v", displayName, err),
			Data:      data,
			Cause:     err,
		}
	}
	return nil
}

func normalizeEvents(cfg componentEvents) componentEvents {
	if cfg.SubmitEvent == "" {
		cfg.SubmitEvent = defaultSubmitEvent
	}
	if cfg.QuitEvent == "" {
		cfg.QuitEvent = defaultQuitEvent
	}
	return cfg
}

type spinnerConfig struct {
	ID          string `attr:"id"`
	Spinner     string `attr:"spinner"`
	Start       bool   `attr:"start" default:"true"`
	ChangeEvent string `attr:"change-event"`
	SubmitEvent string `attr:"submit-event"`
	QuitEvent   string `attr:"quit-event"`
}

func parseSpinnerConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (spinnerConfig, error) {
	cfg := spinnerConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (cfg spinnerConfig) componentType() string { return "spinner" }
func (cfg spinnerConfig) componentID() string   { return cfg.ID }
func (cfg spinnerConfig) newAdapter(programID string) componentAdapter {
	return newSpinnerAdapter(programID, cfg)
}
func (cfg spinnerConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("bubbletea.spinner.type", cfg.Spinner),
	}
}
func (cfg spinnerConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

var spinnerTypes = map[string]spinner.Spinner{
	"line":      spinner.Line,
	"dot":       spinner.Dot,
	"minidot":   spinner.MiniDot,
	"jump":      spinner.Jump,
	"pulse":     spinner.Pulse,
	"points":    spinner.Points,
	"globe":     spinner.Globe,
	"moon":      spinner.Moon,
	"monkey":    spinner.Monkey,
	"meter":     spinner.Meter,
	"hamburger": spinner.Hamburger,
	"ellipsis":  spinner.Ellipsis,
}

type spinnerAdapter struct {
	programID string
	config    spinnerConfig
	model     spinner.Model
}

func newSpinnerAdapter(programID string, cfg spinnerConfig) *spinnerAdapter {
	model := spinner.New()
	if cfg.Spinner != "" {
		if sp, ok := spinnerTypes[strings.ToLower(cfg.Spinner)]; ok {
			model.Spinner = sp
		}
	}
	return &spinnerAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
	}
}

func (m *spinnerAdapter) Type() string { return "spinner" }
func (m *spinnerAdapter) ID() string   { return m.config.ID }
func (m *spinnerAdapter) Init() tea.Cmd {
	if m.config.Start {
		return m.model.Tick
	}
	return nil
}
func (m *spinnerAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)
	return cmd, 0
}
func (m *spinnerAdapter) View() string { return m.model.View() }
func (m *spinnerAdapter) Payload(reason string) map[string]any {
	return map[string]any{
		"component":   "spinner",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"reason":      reason,
	}
}
func (m *spinnerAdapter) CursorPayload() (map[string]any, bool) { return nil, false }

type progressConfig struct {
	ID          string  `attr:"id"`
	Percent     float64 `attr:"percent"`
	Width       int     `attr:"width"`
	ChangeEvent string  `attr:"change-event"`
	SubmitEvent string  `attr:"submit-event"`
	QuitEvent   string  `attr:"quit-event"`
}

func parseProgressConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (progressConfig, error) {
	cfg := progressConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (cfg progressConfig) componentType() string { return "progress" }
func (cfg progressConfig) componentID() string   { return cfg.ID }
func (cfg progressConfig) newAdapter(programID string) componentAdapter {
	return newProgressAdapter(programID, cfg)
}
func (cfg progressConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Float64("bubbletea.progress.percent", cfg.Percent),
		attribute.Int("bubbletea.progress.width", cfg.Width),
	}
}
func (cfg progressConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type progressAdapter struct {
	programID string
	config    progressConfig
	model     progress.Model
	lastPct   float64
}

func newProgressAdapter(programID string, cfg progressConfig) *progressAdapter {
	model := progress.New()
	if cfg.Width > 0 {
		model.Width = cfg.Width
	}
	return &progressAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
		lastPct:   model.Percent(),
	}
}

func (m *progressAdapter) Type() string { return "progress" }
func (m *progressAdapter) ID() string   { return m.config.ID }
func (m *progressAdapter) Init() tea.Cmd {
	if m.config.Percent > 0 {
		cmd := m.model.SetPercent(m.config.Percent)
		m.lastPct = m.model.Percent()
		return cmd
	}
	return nil
}
func (m *progressAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	prev := m.lastPct
	var cmd tea.Cmd
	updated, cmd := m.model.Update(msg)
	if next, ok := updated.(progress.Model); ok {
		m.model = next
	}
	m.lastPct = m.model.Percent()
	if m.lastPct != prev {
		return cmd, flagChanged
	}
	return cmd, 0
}
func (m *progressAdapter) View() string { return m.model.View() }
func (m *progressAdapter) Payload(reason string) map[string]any {
	return map[string]any{
		"component":   "progress",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"percent":     m.model.Percent(),
		"reason":      reason,
	}
}
func (m *progressAdapter) CursorPayload() (map[string]any, bool) { return nil, false }

type paginatorConfig struct {
	ID          string `attr:"id"`
	Type        string `attr:"type"`
	PerPage     int    `attr:"per-page"`
	TotalPages  int    `attr:"total-pages"`
	CursorEvent string `attr:"cursor-event"`
	ChangeEvent string `attr:"change-event"`
	SubmitEvent string `attr:"submit-event"`
	QuitEvent   string `attr:"quit-event"`
}

func parsePaginatorConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (paginatorConfig, error) {
	cfg := paginatorConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (cfg paginatorConfig) componentType() string { return "paginator" }
func (cfg paginatorConfig) componentID() string   { return cfg.ID }
func (cfg paginatorConfig) newAdapter(programID string) componentAdapter {
	return newPaginatorAdapter(programID, cfg)
}
func (cfg paginatorConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int("bubbletea.paginator.per_page", cfg.PerPage),
		attribute.Int("bubbletea.paginator.total_pages", cfg.TotalPages),
		attribute.String("bubbletea.paginator.type", cfg.Type),
	}
}
func (cfg paginatorConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		CursorEvent: cfg.CursorEvent,
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type paginatorAdapter struct {
	programID string
	config    paginatorConfig
	model     paginator.Model
}

func newPaginatorAdapter(programID string, cfg paginatorConfig) *paginatorAdapter {
	opts := []paginator.Option{}
	if cfg.PerPage > 0 {
		opts = append(opts, paginator.WithPerPage(cfg.PerPage))
	}
	if cfg.TotalPages > 0 {
		opts = append(opts, paginator.WithTotalPages(cfg.TotalPages))
	}
	model := paginator.New(opts...)
	switch strings.ToLower(cfg.Type) {
	case "arabic":
		model.Type = paginator.Arabic
	case "dots":
		model.Type = paginator.Dots
	}
	return &paginatorAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
	}
}

func (m *paginatorAdapter) Type() string { return "paginator" }
func (m *paginatorAdapter) ID() string   { return m.config.ID }
func (m *paginatorAdapter) Init() tea.Cmd {
	return nil
}
func (m *paginatorAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	prev := m.model.Page
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)
	if m.model.Page != prev {
		return cmd, flagChanged | flagCursor
	}
	return cmd, 0
}
func (m *paginatorAdapter) View() string { return m.model.View() }
func (m *paginatorAdapter) Payload(reason string) map[string]any {
	return map[string]any{
		"component":   "paginator",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"page":        m.model.Page,
		"perPage":     m.model.PerPage,
		"totalPages":  m.model.TotalPages,
		"reason":      reason,
	}
}
func (m *paginatorAdapter) CursorPayload() (map[string]any, bool) {
	return map[string]any{
		"component":   "paginator",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"page":        m.model.Page,
	}, true
}

type viewportConfig struct {
	ID          string `attr:"id"`
	Width       int    `attr:"width"`
	Height      int    `attr:"height"`
	XOffset     int    `attr:"x-offset"`
	YOffset     int    `attr:"y-offset"`
	Content     string `attr:"content"`
	CursorEvent string `attr:"cursor-event"`
	ChangeEvent string `attr:"change-event"`
	SubmitEvent string `attr:"submit-event"`
	QuitEvent   string `attr:"quit-event"`
}

func parseViewportConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (viewportConfig, error) {
	cfg := viewportConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Content == "" && !hasExprAttribute(el, "content") {
		cfg.Content = strings.TrimSpace(string(el.TextContent()))
	}
	return cfg, nil
}

func (cfg viewportConfig) componentType() string { return "viewport" }
func (cfg viewportConfig) componentID() string   { return cfg.ID }
func (cfg viewportConfig) newAdapter(programID string) componentAdapter {
	return newViewportAdapter(programID, cfg)
}
func (cfg viewportConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int("bubbletea.viewport.width", cfg.Width),
		attribute.Int("bubbletea.viewport.height", cfg.Height),
	}
}
func (cfg viewportConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		CursorEvent: cfg.CursorEvent,
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type viewportAdapter struct {
	programID string
	config    viewportConfig
	model     viewport.Model
	lastX     float64
	lastY     float64
}

func newViewportAdapter(programID string, cfg viewportConfig) *viewportAdapter {
	model := viewport.New(cfg.Width, cfg.Height)
	if cfg.Content != "" {
		model.SetContent(cfg.Content)
	}
	if cfg.XOffset != 0 {
		model.SetXOffset(cfg.XOffset)
	}
	if cfg.YOffset != 0 {
		model.SetYOffset(cfg.YOffset)
	}
	return &viewportAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
		lastX:     model.HorizontalScrollPercent(),
		lastY:     model.ScrollPercent(),
	}
}

func (m *viewportAdapter) Type() string { return "viewport" }
func (m *viewportAdapter) ID() string   { return m.config.ID }
func (m *viewportAdapter) Init() tea.Cmd {
	return nil
}
func (m *viewportAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	switch size := msg.(type) {
	case tea.WindowSizeMsg:
		if m.config.Width == 0 {
			m.model.Width = size.Width
		}
		if m.config.Height == 0 {
			m.model.Height = size.Height
		}
	}
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)
	currX := m.model.HorizontalScrollPercent()
	currY := m.model.ScrollPercent()
	if currX != m.lastX || currY != m.lastY {
		m.lastX = currX
		m.lastY = currY
		return cmd, flagCursor
	}
	return cmd, 0
}
func (m *viewportAdapter) View() string { return m.model.View() }
func (m *viewportAdapter) Payload(reason string) map[string]any {
	return map[string]any{
		"component":               "viewport",
		"programId":               m.programID,
		"componentId":             m.config.ID,
		"scrollPercent":           m.model.ScrollPercent(),
		"horizontalScrollPercent": m.model.HorizontalScrollPercent(),
		"atTop":                   m.model.AtTop(),
		"atBottom":                m.model.AtBottom(),
		"reason":                  reason,
	}
}
func (m *viewportAdapter) CursorPayload() (map[string]any, bool) {
	return map[string]any{
		"component":               "viewport",
		"programId":               m.programID,
		"componentId":             m.config.ID,
		"scrollPercent":           m.model.ScrollPercent(),
		"horizontalScrollPercent": m.model.HorizontalScrollPercent(),
	}, true
}

type textInputConfig struct {
	ID          string   `attr:"id"`
	Placeholder string   `attr:"placeholder"`
	Prompt      string   `attr:"prompt"`
	Value       string   `attr:"value"`
	Width       int      `attr:"width"`
	CharLimit   int      `attr:"char-limit"`
	EchoMode    string   `attr:"echo-mode"`
	Focused     bool     `attr:"focused"`
	Suggestions []string `attr:"suggestions"`
	CursorEvent string   `attr:"cursor-event"`
	ChangeEvent string   `attr:"change-event"`
	SubmitEvent string   `attr:"submit-event"`
	QuitEvent   string   `attr:"quit-event"`
}

func parseTextInputConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (textInputConfig, error) {
	cfg := textInputConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Value == "" && !hasExprAttribute(el, "value") {
		cfg.Value = strings.TrimSpace(string(el.TextContent()))
	}
	return cfg, nil
}

func (cfg textInputConfig) componentType() string { return "textinput" }
func (cfg textInputConfig) componentID() string   { return cfg.ID }
func (cfg textInputConfig) newAdapter(programID string) componentAdapter {
	return newTextInputAdapter(programID, cfg)
}
func (cfg textInputConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int("bubbletea.textinput.width", cfg.Width),
		attribute.Int("bubbletea.textinput.char_limit", cfg.CharLimit),
	}
}
func (cfg textInputConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		CursorEvent: cfg.CursorEvent,
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type textInputAdapter struct {
	programID string
	config    textInputConfig
	model     textinput.Model
}

func newTextInputAdapter(programID string, cfg textInputConfig) *textInputAdapter {
	model := textinput.New()
	if cfg.Prompt != "" {
		model.Prompt = cfg.Prompt
	}
	if cfg.Placeholder != "" {
		model.Placeholder = cfg.Placeholder
	}
	if cfg.Width > 0 {
		model.Width = cfg.Width
	}
	if cfg.CharLimit > 0 {
		model.CharLimit = cfg.CharLimit
	}
	if cfg.Value != "" {
		model.SetValue(cfg.Value)
	}
	if len(cfg.Suggestions) > 0 {
		model.SetSuggestions(cfg.Suggestions)
	}
	switch strings.ToLower(cfg.EchoMode) {
	case "none":
		model.EchoMode = textinput.EchoNone
	case "password":
		model.EchoMode = textinput.EchoPassword
	}
	if cfg.Focused {
		model.Focus()
	}
	return &textInputAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
	}
}

func (m *textInputAdapter) Type() string { return "textinput" }
func (m *textInputAdapter) ID() string   { return m.config.ID }
func (m *textInputAdapter) Init() tea.Cmd {
	if m.config.Focused {
		return m.model.Focus()
	}
	return nil
}
func (m *textInputAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	prevValue := m.model.Value()
	prevCursor := m.model.Position()
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)

	flags := updateFlags(0)
	if m.model.Value() != prevValue {
		flags |= flagChanged
	}
	if m.model.Position() != prevCursor {
		flags |= flagCursor
	}
	if isEnterKey(msg) {
		flags |= flagSubmitted
	}
	return cmd, flags
}
func (m *textInputAdapter) View() string { return m.model.View() }
func (m *textInputAdapter) Payload(reason string) map[string]any {
	return map[string]any{
		"component":   "textinput",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"value":       m.model.Value(),
		"cursorIndex": m.model.Position(),
		"reason":      reason,
	}
}
func (m *textInputAdapter) CursorPayload() (map[string]any, bool) {
	return map[string]any{
		"component":   "textinput",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"cursorIndex": m.model.Position(),
		"value":       m.model.Value(),
	}, true
}

type textAreaConfig struct {
	ID              string `attr:"id"`
	Placeholder     string `attr:"placeholder"`
	Prompt          string `attr:"prompt"`
	Value           string `attr:"value"`
	Width           int    `attr:"width"`
	Height          int    `attr:"height"`
	ShowLineNumbers bool   `attr:"show-line-numbers"`
	Focused         bool   `attr:"focused"`
	CursorEvent     string `attr:"cursor-event"`
	ChangeEvent     string `attr:"change-event"`
	SubmitEvent     string `attr:"submit-event"`
	QuitEvent       string `attr:"quit-event"`
}

func parseTextAreaConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (textAreaConfig, error) {
	cfg := textAreaConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Value == "" && !hasExprAttribute(el, "value") {
		cfg.Value = strings.TrimSpace(string(el.TextContent()))
	}
	return cfg, nil
}

func (cfg textAreaConfig) componentType() string { return "textarea" }
func (cfg textAreaConfig) componentID() string   { return cfg.ID }
func (cfg textAreaConfig) newAdapter(programID string) componentAdapter {
	return newTextAreaAdapter(programID, cfg)
}
func (cfg textAreaConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int("bubbletea.textarea.width", cfg.Width),
		attribute.Int("bubbletea.textarea.height", cfg.Height),
	}
}
func (cfg textAreaConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		CursorEvent: cfg.CursorEvent,
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type textAreaAdapter struct {
	programID string
	config    textAreaConfig
	model     textarea.Model
}

func newTextAreaAdapter(programID string, cfg textAreaConfig) *textAreaAdapter {
	model := textarea.New()
	if cfg.Prompt != "" {
		model.Prompt = cfg.Prompt
	}
	if cfg.Placeholder != "" {
		model.Placeholder = cfg.Placeholder
	}
	if cfg.Width > 0 {
		model.SetWidth(cfg.Width)
	}
	if cfg.Height > 0 {
		model.SetHeight(cfg.Height)
	}
	if cfg.Value != "" {
		model.SetValue(cfg.Value)
	}
	if cfg.ShowLineNumbers {
		model.ShowLineNumbers = true
	}
	if cfg.Focused {
		model.Focus()
	}
	return &textAreaAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
	}
}

func (m *textAreaAdapter) Type() string { return "textarea" }
func (m *textAreaAdapter) ID() string   { return m.config.ID }
func (m *textAreaAdapter) Init() tea.Cmd {
	if m.config.Focused {
		return m.model.Focus()
	}
	return nil
}
func (m *textAreaAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	prevValue := m.model.Value()
	prevCursor := m.model.LineInfo()
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)

	flags := updateFlags(0)
	if m.model.Value() != prevValue {
		flags |= flagChanged
	}
	if m.model.LineInfo() != prevCursor {
		flags |= flagCursor
	}
	if isEnterKey(msg) {
		flags |= flagSubmitted
	}
	return cmd, flags
}
func (m *textAreaAdapter) View() string { return m.model.View() }
func (m *textAreaAdapter) Payload(reason string) map[string]any {
	line := m.model.Line()
	info := m.model.LineInfo()
	return map[string]any{
		"component":   "textarea",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"value":       m.model.Value(),
		"line":        line,
		"column":      info.ColumnOffset,
		"rowOffset":   info.RowOffset,
		"reason":      reason,
	}
}
func (m *textAreaAdapter) CursorPayload() (map[string]any, bool) {
	line := m.model.Line()
	info := m.model.LineInfo()
	return map[string]any{
		"component":   "textarea",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"line":        line,
		"column":      info.ColumnOffset,
		"rowOffset":   info.RowOffset,
	}, true
}

type tableConfig struct {
	ID          string `attr:"id"`
	Width       int    `attr:"width"`
	Height      int    `attr:"height"`
	Focused     bool   `attr:"focused"`
	CursorEvent string `attr:"cursor-event"`
	ChangeEvent string `attr:"change-event"`
	SubmitEvent string `attr:"submit-event"`
	QuitEvent   string `attr:"quit-event"`
	Columns     []table.Column
	Rows        []table.Row
}

type tableColumnConfig struct {
	Title string `attr:"title"`
	Width int    `attr:"width"`
}

func parseTableConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (tableConfig, error) {
	cfg := tableConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}

	columns, rows, err := parseTableChildren(ctx, el, displayName, itp)
	if err != nil {
		return cfg, err
	}
	cfg.Columns = columns
	cfg.Rows = rows

	if len(cfg.Columns) == 0 {
		return cfg, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("%s requires at least one bubbletea:column", displayName),
			Data: map[string]any{
				"element": displayName,
			},
		}
	}
	return cfg, nil
}

func parseTableChildren(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) ([]table.Column, []table.Row, error) {
	var columns []table.Column
	var rows []table.Row

	children := el.ChildNodes()
	for i := uint(0); i < children.Length(); i++ {
		childEl, ok := children.Item(i).(xmldom.Element)
		if !ok {
			continue
		}
		switch strings.ToLower(string(childEl.LocalName())) {
		case "columns":
			cols, err := parseTableColumns(ctx, childEl, displayName, itp)
			if err != nil {
				return nil, nil, err
			}
			columns = append(columns, cols...)
		case "rows":
			rs, err := parseTableRows(ctx, childEl, displayName, itp)
			if err != nil {
				return nil, nil, err
			}
			rows = append(rows, rs...)
		}
	}

	return columns, rows, nil
}

func parseTableColumns(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) ([]table.Column, error) {
	var columns []table.Column
	children := el.ChildNodes()
	for i := uint(0); i < children.Length(); i++ {
		childEl, ok := children.Item(i).(xmldom.Element)
		if !ok {
			continue
		}
		if !equalsLocalName(childEl, "column") {
			continue
		}
		colCfg := tableColumnConfig{}
		if err := bindComponentConfig(ctx, childEl, displayName, itp, &colCfg); err != nil {
			return nil, err
		}
		if strings.TrimSpace(colCfg.Title) == "" {
			return nil, &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("%s column requires a title", displayName),
				Data: map[string]any{
					"element": displayName,
				},
			}
		}
		columns = append(columns, table.Column{
			Title: colCfg.Title,
			Width: colCfg.Width,
		})
	}
	return columns, nil
}

func parseTableRows(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) ([]table.Row, error) {
	var rows []table.Row
	children := el.ChildNodes()
	for i := uint(0); i < children.Length(); i++ {
		childEl, ok := children.Item(i).(xmldom.Element)
		if !ok {
			continue
		}
		if !equalsLocalName(childEl, "row") {
			continue
		}
		row, err := parseTableRow(ctx, childEl, displayName, itp)
		if err != nil {
			return nil, err
		}
		if len(row) == 0 {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseTableRow(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (table.Row, error) {
	cells := make([]string, 0)
	children := el.ChildNodes()
	for i := uint(0); i < children.Length(); i++ {
		childEl, ok := children.Item(i).(xmldom.Element)
		if !ok {
			continue
		}
		if !equalsLocalName(childEl, "cell") {
			continue
		}
		cell := ""
		if expr := strings.TrimSpace(string(childEl.GetAttribute("expr"))); expr != "" {
			val, err := evalExprString(ctx, itp, displayName, "expr", expr)
			if err != nil {
				return nil, err
			}
			cell = val
		} else {
			cell = strings.TrimSpace(string(childEl.TextContent()))
		}
		cells = append(cells, cell)
	}
	if len(cells) == 0 {
		content := strings.TrimSpace(string(el.TextContent()))
		if content == "" {
			return nil, nil
		}
		sep := strings.TrimSpace(string(el.GetAttribute("separator")))
		if sep == "" {
			if exprAttr, expr := lookupExprAttribute(el, "separator"); exprAttr != "" {
				val, err := evalExprString(ctx, itp, displayName, exprAttr, expr)
				if err != nil {
					return nil, err
				}
				sep = val
			}
		}
		if sep == "" {
			sep = "|"
		}
		for _, part := range strings.Split(content, sep) {
			cell := strings.TrimSpace(part)
			if cell == "" {
				continue
			}
			cells = append(cells, cell)
		}
	}
	return table.Row(cells), nil
}

func (cfg tableConfig) componentType() string { return "table" }
func (cfg tableConfig) componentID() string   { return cfg.ID }
func (cfg tableConfig) newAdapter(programID string) componentAdapter {
	return newTableAdapter(programID, cfg)
}
func (cfg tableConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int("bubbletea.table.columns", len(cfg.Columns)),
		attribute.Int("bubbletea.table.rows", len(cfg.Rows)),
	}
}
func (cfg tableConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		CursorEvent: cfg.CursorEvent,
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type tableAdapter struct {
	programID string
	config    tableConfig
	model     table.Model
}

func newTableAdapter(programID string, cfg tableConfig) *tableAdapter {
	opts := []table.Option{
		table.WithColumns(cfg.Columns),
		table.WithRows(cfg.Rows),
	}
	if cfg.Height > 0 {
		opts = append(opts, table.WithHeight(cfg.Height))
	}
	if cfg.Width > 0 {
		opts = append(opts, table.WithWidth(cfg.Width))
	}
	if cfg.Focused {
		opts = append(opts, table.WithFocused(true))
	}
	model := table.New(opts...)
	return &tableAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
	}
}

func (m *tableAdapter) Type() string { return "table" }
func (m *tableAdapter) ID() string   { return m.config.ID }
func (m *tableAdapter) Init() tea.Cmd {
	return nil
}
func (m *tableAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	prev := m.model.Cursor()
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)

	flags := updateFlags(0)
	if m.model.Cursor() != prev {
		flags |= flagCursor | flagChanged
	}
	if isEnterKey(msg) {
		flags |= flagSubmitted
	}
	return cmd, flags
}
func (m *tableAdapter) View() string { return m.model.View() }
func (m *tableAdapter) Payload(reason string) map[string]any {
	row := m.model.SelectedRow()
	return map[string]any{
		"component":   "table",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"cursorIndex": m.model.Cursor(),
		"row":         []string(row),
		"reason":      reason,
	}
}
func (m *tableAdapter) CursorPayload() (map[string]any, bool) {
	row := m.model.SelectedRow()
	return map[string]any{
		"component":   "table",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"cursorIndex": m.model.Cursor(),
		"row":         []string(row),
	}, true
}

type filePickerConfig struct {
	ID               string   `attr:"id"`
	Height           int      `attr:"height"`
	CurrentDirectory string   `attr:"current-directory"`
	AllowedTypes     []string `attr:"allowed-types"`
	ShowHidden       bool     `attr:"show-hidden"`
	ShowSize         bool     `attr:"show-size"`
	ShowPermissions  bool     `attr:"show-permissions"`
	DirAllowed       bool     `attr:"dir-allowed" default:"true"`
	FileAllowed      bool     `attr:"file-allowed" default:"true"`
	AutoHeight       bool     `attr:"auto-height"`
	Cursor           string   `attr:"cursor"`
	ChangeEvent      string   `attr:"change-event"`
	SubmitEvent      string   `attr:"submit-event"`
	QuitEvent        string   `attr:"quit-event"`
}

func parseFilePickerConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (filePickerConfig, error) {
	cfg := filePickerConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (cfg filePickerConfig) componentType() string { return "filepicker" }
func (cfg filePickerConfig) componentID() string   { return cfg.ID }
func (cfg filePickerConfig) newAdapter(programID string) componentAdapter {
	return newFilePickerAdapter(programID, cfg)
}
func (cfg filePickerConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int("bubbletea.filepicker.height", cfg.Height),
		attribute.StringSlice("bubbletea.filepicker.allowed_types", cfg.AllowedTypes),
	}
}
func (cfg filePickerConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type filePickerAdapter struct {
	programID string
	config    filePickerConfig
	model     filepicker.Model
	lastDir   string
	lastPath  string
}

func newFilePickerAdapter(programID string, cfg filePickerConfig) *filePickerAdapter {
	model := filepicker.New()
	if cfg.Height > 0 {
		model.SetHeight(cfg.Height)
	}
	if cfg.CurrentDirectory != "" {
		model.CurrentDirectory = cfg.CurrentDirectory
	}
	if len(cfg.AllowedTypes) > 0 {
		model.AllowedTypes = cfg.AllowedTypes
	}
	model.ShowHidden = cfg.ShowHidden
	model.ShowSize = cfg.ShowSize
	model.ShowPermissions = cfg.ShowPermissions
	model.DirAllowed = cfg.DirAllowed
	model.FileAllowed = cfg.FileAllowed
	model.AutoHeight = cfg.AutoHeight
	if cfg.Cursor != "" {
		model.Cursor = cfg.Cursor
	}
	return &filePickerAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
		lastDir:   model.CurrentDirectory,
		lastPath:  model.Path,
	}
}

func (m *filePickerAdapter) Type() string { return "filepicker" }
func (m *filePickerAdapter) ID() string   { return m.config.ID }
func (m *filePickerAdapter) Init() tea.Cmd {
	return m.model.Init()
}
func (m *filePickerAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)

	flags := updateFlags(0)
	if m.model.CurrentDirectory != m.lastDir || m.model.Path != m.lastPath {
		m.lastDir = m.model.CurrentDirectory
		m.lastPath = m.model.Path
		flags |= flagChanged
	}
	if selected, _ := m.model.DidSelectFile(msg); selected {
		flags |= flagSubmitted
	}
	return cmd, flags
}
func (m *filePickerAdapter) View() string { return m.model.View() }
func (m *filePickerAdapter) Payload(reason string) map[string]any {
	return map[string]any{
		"component":        "filepicker",
		"programId":        m.programID,
		"componentId":      m.config.ID,
		"path":             m.model.Path,
		"currentDirectory": m.model.CurrentDirectory,
		"reason":           reason,
	}
}
func (m *filePickerAdapter) CursorPayload() (map[string]any, bool) { return nil, false }

type timerConfig struct {
	ID          string        `attr:"id"`
	Timeout     time.Duration `attr:"timeout"`
	Interval    time.Duration `attr:"interval"`
	AutoStart   bool          `attr:"autostart" default:"true"`
	ChangeEvent string        `attr:"change-event"`
	SubmitEvent string        `attr:"submit-event"`
	QuitEvent   string        `attr:"quit-event"`
}

func parseTimerConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (timerConfig, error) {
	cfg := timerConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Timeout <= 0 {
		return cfg, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("%s requires a timeout duration", displayName),
			Data: map[string]any{
				"element": displayName,
			},
		}
	}
	return cfg, nil
}

func (cfg timerConfig) componentType() string { return "timer" }
func (cfg timerConfig) componentID() string   { return cfg.ID }
func (cfg timerConfig) newAdapter(programID string) componentAdapter {
	return newTimerAdapter(programID, cfg)
}
func (cfg timerConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("bubbletea.timer.timeout", cfg.Timeout.String()),
		attribute.String("bubbletea.timer.interval", cfg.Interval.String()),
	}
}
func (cfg timerConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type timerAdapter struct {
	programID string
	config    timerConfig
	model     timer.Model
	timedOut  bool
}

func newTimerAdapter(programID string, cfg timerConfig) *timerAdapter {
	var model timer.Model
	if cfg.Interval > 0 {
		model = timer.NewWithInterval(cfg.Timeout, cfg.Interval)
	} else {
		model = timer.New(cfg.Timeout)
	}
	return &timerAdapter{
		programID: programID,
		config:    cfg,
		model:     model,
		timedOut:  model.Timedout(),
	}
}

func (m *timerAdapter) Type() string { return "timer" }
func (m *timerAdapter) ID() string   { return m.config.ID }
func (m *timerAdapter) Init() tea.Cmd {
	if m.config.AutoStart {
		return m.model.Start()
	}
	return nil
}
func (m *timerAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)
	if !m.timedOut && m.model.Timedout() {
		m.timedOut = true
		return cmd, flagSubmitted
	}
	return cmd, 0
}
func (m *timerAdapter) View() string { return m.model.View() }
func (m *timerAdapter) Payload(reason string) map[string]any {
	return map[string]any{
		"component":   "timer",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"timedOut":    m.model.Timedout(),
		"timeout":     m.config.Timeout.String(),
		"reason":      reason,
	}
}
func (m *timerAdapter) CursorPayload() (map[string]any, bool) { return nil, false }

type stopwatchConfig struct {
	ID          string        `attr:"id"`
	Interval    time.Duration `attr:"interval"`
	AutoStart   bool          `attr:"autostart" default:"true"`
	ChangeEvent string        `attr:"change-event"`
	SubmitEvent string        `attr:"submit-event"`
	QuitEvent   string        `attr:"quit-event"`
}

func parseStopwatchConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (stopwatchConfig, error) {
	cfg := stopwatchConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (cfg stopwatchConfig) componentType() string { return "stopwatch" }
func (cfg stopwatchConfig) componentID() string   { return cfg.ID }
func (cfg stopwatchConfig) newAdapter(programID string) componentAdapter {
	return newStopwatchAdapter(programID, cfg)
}
func (cfg stopwatchConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("bubbletea.stopwatch.interval", cfg.Interval.String()),
	}
}
func (cfg stopwatchConfig) events() componentEvents {
	return normalizeEvents(componentEvents{
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	})
}

type stopwatchAdapter struct {
	programID   string
	config      stopwatchConfig
	model       stopwatch.Model
	lastElapsed time.Duration
}

func newStopwatchAdapter(programID string, cfg stopwatchConfig) *stopwatchAdapter {
	var model stopwatch.Model
	if cfg.Interval > 0 {
		model = stopwatch.NewWithInterval(cfg.Interval)
	} else {
		model = stopwatch.New()
	}
	return &stopwatchAdapter{
		programID:   programID,
		config:      cfg,
		model:       model,
		lastElapsed: model.Elapsed(),
	}
}

func (m *stopwatchAdapter) Type() string { return "stopwatch" }
func (m *stopwatchAdapter) ID() string   { return m.config.ID }
func (m *stopwatchAdapter) Init() tea.Cmd {
	if m.config.AutoStart {
		return m.model.Start()
	}
	return nil
}
func (m *stopwatchAdapter) Update(msg tea.Msg) (tea.Cmd, updateFlags) {
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)
	elapsed := m.model.Elapsed()
	if elapsed != m.lastElapsed {
		m.lastElapsed = elapsed
		return cmd, flagChanged
	}
	return cmd, 0
}
func (m *stopwatchAdapter) View() string { return m.model.View() }
func (m *stopwatchAdapter) Payload(reason string) map[string]any {
	return map[string]any{
		"component":   "stopwatch",
		"programId":   m.programID,
		"componentId": m.config.ID,
		"elapsed":     m.model.Elapsed().String(),
		"reason":      reason,
	}
}
func (m *stopwatchAdapter) CursorPayload() (map[string]any, bool) { return nil, false }

func init() {
	registerComponent("spinner", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseSpinnerConfig(ctx, el, displayName, itp)
	})
	registerComponent("progress", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseProgressConfig(ctx, el, displayName, itp)
	})
	registerComponent("paginator", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parsePaginatorConfig(ctx, el, displayName, itp)
	})
	registerComponent("viewport", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseViewportConfig(ctx, el, displayName, itp)
	})
	registerComponent("textinput", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseTextInputConfig(ctx, el, displayName, itp)
	})
	registerComponent("textarea", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseTextAreaConfig(ctx, el, displayName, itp)
	})
	registerComponent("table", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseTableConfig(ctx, el, displayName, itp)
	})
	registerComponent("filepicker", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseFilePickerConfig(ctx, el, displayName, itp)
	})
	registerComponent("timer", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseTimerConfig(ctx, el, displayName, itp)
	})
	registerComponent("stopwatch", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseStopwatchConfig(ctx, el, displayName, itp)
	})
}
