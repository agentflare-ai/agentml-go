package bubbletea

import (
	"context"
	"fmt"
	"strconv"
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
	ID          string
	Title       string
	Multi       bool
	CursorEvent string
	ChangeEvent string
	SubmitEvent string
	QuitEvent   string
	Items       []listItemConfig
}

type listItemConfig struct {
	Label string
	Value string
}

func newProgramExecutable(el xmldom.Element, mgr *Manager) (*programExecutable, error) {
	cfg, err := parseProgramConfig(el)
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

func parseProgramConfig(el xmldom.Element) (ProgramConfig, error) {
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

	componentCfg, err := parser(componentEl, displayName)
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

func parseListConfig(el xmldom.Element, displayName string) (listConfig, error) {
	cfg := listConfig{
		ID:          strings.TrimSpace(string(el.GetAttribute("id"))),
		Title:       strings.TrimSpace(string(el.GetAttribute("title"))),
		CursorEvent: strings.TrimSpace(string(el.GetAttribute("cursor-event"))),
		ChangeEvent: strings.TrimSpace(string(el.GetAttribute("change-event"))),
		SubmitEvent: strings.TrimSpace(string(el.GetAttribute("submit-event"))),
		QuitEvent:   strings.TrimSpace(string(el.GetAttribute("quit-event"))),
	}

	if cfg.SubmitEvent == "" {
		cfg.SubmitEvent = defaultSubmitEvent
	}
	if cfg.QuitEvent == "" {
		cfg.QuitEvent = defaultQuitEvent
	}

	if multiAttr := strings.TrimSpace(string(el.GetAttribute("multi"))); multiAttr != "" {
		parsed, err := strconv.ParseBool(multiAttr)
		if err != nil {
			return cfg, &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("%s multi attribute invalid: %v", displayName, err),
				Data: map[string]any{
					"element": displayName,
					"value":   multiAttr,
				},
				Cause: err,
			}
		}
		cfg.Multi = parsed
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
		item := strings.TrimSpace(string(childEl.TextContent()))
		if item == "" {
			continue
		}
		cfg.Items = append(cfg.Items, listItemConfig{
			Label: item,
			Value: strings.TrimSpace(string(childEl.GetAttribute("value"))),
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
	registerComponent("list", func(el xmldom.Element, displayName string) (componentConfig, error) {
		return parseListConfig(el, displayName)
	})
}
