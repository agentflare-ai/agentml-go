package bubbletea

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-muid"
	"github.com/agentflare-ai/go-xmldom"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultSubmitEvent = "bubbletea.submit"
	defaultQuitEvent   = "bubbletea.quit"
)

type programExecutable struct {
	element xmldom.Element
	manager *Manager
	config  ProgramConfig
}

// ProgramConfig captures the declarative instructions for a Bubble Tea UI.
type ProgramConfig struct {
	ProgramID string
	component componentConfig
}

type listConfig struct {
	ID          string `attr:"id"`
	Title       string `attr:"title"`
	Multi       bool   `attr:"multi"`
	CursorEvent string `attr:"cursor-event"`
	ChangeEvent string `attr:"change-event"`
	SubmitEvent string `attr:"submit-event"`
	QuitEvent   string `attr:"quit-event"`
	Items       []listItemConfig
}

type listItemConfig struct {
	Label string
	Value string
}

func newProgramExecutable(ctx context.Context, el xmldom.Element, mgr *Manager, itp agentml.Interpreter) (*programExecutable, error) {
	cfg, err := parseProgramConfig(ctx, el, itp)
	if err != nil {
		return nil, err
	}
	return &programExecutable{
		element: el,
		manager: mgr,
		config:  cfg,
	}, nil
}

func (p *programExecutable) Execute(ctx context.Context, itp agentml.Interpreter) error {
	if p.config.component == nil {
		return fmt.Errorf("bubbletea: program has no component configured")
	}

	ctx, span := tracer.Start(ctx, "bubbletea.program.execute",
		trace.WithAttributes(
			append([]attribute.KeyValue{
				attribute.String("bubbletea.program.id", p.config.ProgramID),
				attribute.String("bubbletea.component.type", p.config.component.componentType()),
			}, p.config.component.spanAttributes()...)...,
		))
	defer span.End()

	if _, err := p.manager.Start(ctx, p.config, itp); err != nil {
		return err
	}
	return nil
}

func parseProgramConfig(ctx context.Context, el xmldom.Element, itp agentml.Interpreter) (ProgramConfig, error) {
	cfg := ProgramConfig{
		ProgramID: strings.TrimSpace(string(el.GetAttribute("id"))),
	}

	componentEl := findFirstElementChild(el)
	if componentEl == nil {
		return cfg, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "bubbletea:program requires a bubbletea component child (e.g., bubbletea:list)",
			Data: map[string]any{
				"element":   "bubbletea:program",
				"supported": registeredComponentNames(),
			},
		}
	}

	componentType, displayName, err := resolveComponentType(componentEl)
	if err != nil {
		return cfg, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   err.Error(),
			Data: map[string]any{
				"element": "bubbletea:program",
			},
			Cause: err,
		}
	}

	parser, ok := lookupComponent(componentType)
	if !ok {
		return cfg, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("bubbletea component %q is not supported", componentType),
			Data: map[string]any{
				"element":   displayName,
				"supported": registeredComponentNames(),
			},
		}
	}

	componentCfg, err := parser(ctx, componentEl, displayName, itp)
	if err != nil {
		return cfg, err
	}
	cfg.component = componentCfg

	if cfg.ProgramID == "" {
		// ID derived from muid.String().
		cfg.ProgramID = muid.MakeString()
	}

	return cfg, nil
}

func parseListConfig(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (listConfig, error) {
	cfg := listConfig{}
	if err := bindComponentConfig(ctx, el, displayName, itp, &cfg); err != nil {
		return cfg, err
	}

	if cfg.SubmitEvent == "" {
		cfg.SubmitEvent = defaultSubmitEvent
	}
	if cfg.QuitEvent == "" {
		cfg.QuitEvent = defaultQuitEvent
	}

	childNodes := el.ChildNodes()
	for i := uint(0); i < childNodes.Length(); i++ {
		childEl, ok := childNodes.Item(i).(xmldom.Element)
		if !ok {
			continue
		}
		if !equalsLocalName(childEl, "item") {
			continue
		}
		itemLabel, err := resolveItemLabel(ctx, childEl, displayName, itp)
		if err != nil {
			return cfg, err
		}
		if itemLabel == "" {
			continue
		}
		itemValue, err := resolveItemValue(ctx, childEl, displayName, itp)
		if err != nil {
			return cfg, err
		}
		cfg.Items = append(cfg.Items, listItemConfig{
			Label: itemLabel,
			Value: itemValue,
		})
	}

	if len(cfg.Items) == 0 {
		return cfg, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("%s requires at least one bubbletea:item child", displayName),
			Data: map[string]any{
				"element": displayName,
			},
		}
	}

	return cfg, nil
}

func findFirstElementChild(el xmldom.Element) xmldom.Element {
	children := el.ChildNodes()
	for i := uint(0); i < children.Length(); i++ {
		childEl, ok := children.Item(i).(xmldom.Element)
		if !ok {
			continue
		}
		return childEl
	}
	return nil
}

func equalsLocalName(el xmldom.Element, name string) bool {
	return strings.EqualFold(string(el.LocalName()), name)
}

func (cfg listConfig) componentType() string {
	return "list"
}

func (cfg listConfig) componentID() string {
	return cfg.ID
}

func (cfg listConfig) newAdapter(programID string) componentAdapter {
	return newListAdapter(programID, cfg)
}

func (cfg listConfig) spanAttributes() []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int("bubbletea.list.items", len(cfg.Items)),
		attribute.Bool("bubbletea.list.multi", cfg.Multi),
	}
}

func (cfg listConfig) events() componentEvents {
	return componentEvents{
		CursorEvent: cfg.CursorEvent,
		ChangeEvent: cfg.ChangeEvent,
		SubmitEvent: cfg.SubmitEvent,
		QuitEvent:   cfg.QuitEvent,
	}
}

func init() {
	registerComponent("list", func(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (componentConfig, error) {
		return parseListConfig(ctx, el, displayName, itp)
	})
}

func resolveItemLabel(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (string, error) {
	if exprAttr, expr := lookupExprAttribute(el, "label"); exprAttr != "" {
		return evalExprString(ctx, itp, displayName, exprAttr, expr)
	}
	if expr := strings.TrimSpace(string(el.GetAttribute("expr"))); expr != "" {
		return evalExprString(ctx, itp, displayName, "expr", expr)
	}
	return strings.TrimSpace(string(el.TextContent())), nil
}

func resolveItemValue(ctx context.Context, el xmldom.Element, displayName string, itp agentml.Interpreter) (string, error) {
	if exprAttr, expr := lookupExprAttribute(el, "value"); exprAttr != "" {
		return evalExprString(ctx, itp, displayName, exprAttr, expr)
	}
	value := strings.TrimSpace(string(el.GetAttribute("value")))
	if value != "" {
		return value, nil
	}
	label, err := resolveItemLabel(ctx, el, displayName, itp)
	if err != nil {
		return "", err
	}
	return label, nil
}

func evalExprString(ctx context.Context, itp agentml.Interpreter, displayName, attrName, expr string) (string, error) {
	if itp == nil || itp.DataModel() == nil {
		return "", newAttrEvalError(displayName, attrName, expr, errNoDataModel)
	}
	val, err := itp.DataModel().EvaluateValue(ctx, expr)
	if err != nil {
		return "", newAttrEvalError(displayName, attrName, expr, err)
	}
	if val == nil {
		return "", nil
	}
	return fmt.Sprintf("%v", val), nil
}

func newAttrEvalError(displayName, attrName, expr string, err error) error {
	attrErr := &attrError{Attr: attrName, Value: expr, Cause: err}
	return &agentml.PlatformError{
		EventName: "error.execution",
		Message:   fmt.Sprintf("%s %v", displayName, attrErr),
		Data: map[string]any{
			"element":   displayName,
			"attribute": attrName,
			"value":     expr,
		},
		Cause: err,
	}
}
