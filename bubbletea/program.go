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
	List      listConfig
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
	ctx, span := tracer.Start(ctx, "bubbletea.program.execute",
		trace.WithAttributes(
			attribute.String("bubbletea.program.id", p.config.ProgramID),
			attribute.Int("bubbletea.list.items", len(p.config.List.Items)),
			attribute.Bool("bubbletea.list.multi", p.config.List.Multi),
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

	listEl := findFirstChild(el, "list")
	if listEl == nil {
		return cfg, &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "bubbletea:program requires a bubbletea:list child",
			Data: map[string]any{
				"element": "bubbletea:program",
			},
		}
	}

	listCfg, err := parseListConfig(listEl)
	if err != nil {
		return cfg, err
	}
	cfg.List = listCfg

	if cfg.ProgramID == "" {
		// ID derived from muid.String().
		cfg.ProgramID = muid.MakeString()
	}

	return cfg, nil
}

func parseListConfig(el xmldom.Element) (listConfig, error) {
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
				Message:   fmt.Sprintf("bubbletea:list multi attribute invalid: %v", err),
				Data: map[string]any{
					"element": "bubbletea:list",
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
			Message:   "bubbletea:list requires at least one bubbletea:item child",
			Data: map[string]any{
				"element": "bubbletea:list",
			},
		}
	}

	return cfg, nil
}

func findFirstChild(el xmldom.Element, local string) xmldom.Element {
	children := el.ChildNodes()
	for i := uint(0); i < children.Length(); i++ {
		childEl, ok := children.Item(i).(xmldom.Element)
		if !ok {
			continue
		}
		if equalsLocalName(childEl, local) {
			return childEl
		}
	}
	return nil
}

func equalsLocalName(el xmldom.Element, name string) bool {
	return strings.EqualFold(string(el.LocalName()), name)
}
