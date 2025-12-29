package bubbletea

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/agentflare-ai/agentml-go"
	tea "github.com/charmbracelet/bubbletea"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/term"
)

var tracer = otel.Tracer("github.com/agentflare-ai/agentml-go/bubbletea")

// Manager supervises Bubble Tea programs.
type Manager struct {
	mu        sync.Mutex
	programs  map[string]*tea.Program
	terminate map[string]context.CancelFunc
}

// NewManager constructs an empty manager.
func NewManager() *Manager {
	return &Manager{
		programs:  make(map[string]*tea.Program),
		terminate: make(map[string]context.CancelFunc),
	}
}

// Start launches a Bubble Tea program asynchronously.
func (m *Manager) Start(ctx context.Context, cfg ProgramConfig, itp agentml.Interpreter) (string, error) {
	if cfg.component == nil {
		return "", fmt.Errorf("bubbletea: program has no component configured")
	}

	ctx, span := tracer.Start(ctx, "bubbletea.manager.start",
		trace.WithAttributes(
			append([]attribute.KeyValue{
				attribute.String("bubbletea.program.id", cfg.ProgramID),
				attribute.String("bubbletea.component.type", cfg.component.componentType()),
			}, cfg.component.spanAttributes()...)...,
		))
	defer span.End()

	modelCtx, cancel := context.WithCancel(ctx)
	adapter := cfg.component.newAdapter(cfg.ProgramID)
	model := newBaseModel(modelCtx, cfg.ProgramID, adapter, cfg.component.events(), itp)
	options := []tea.ProgramOption{tea.WithContext(modelCtx)}
	if !isTTY() {
		options = append(options, tea.WithoutRenderer())
	}

	program := tea.NewProgram(model, options...)

	m.mu.Lock()
	m.programs[cfg.ProgramID] = program
	m.terminate[cfg.ProgramID] = cancel
	m.mu.Unlock()

	go func(id string) {
		defer func() {
			m.mu.Lock()
			delete(m.programs, id)
			if closer, ok := m.terminate[id]; ok {
				closer()
				delete(m.terminate, id)
			}
			m.mu.Unlock()
		}()
		if _, err := program.Run(); err != nil {
			slog.ErrorContext(ctx, "bubbletea: program run failed",
				"program_id", id,
				"error", err)
		}
	}(cfg.ProgramID)

	return cfg.ProgramID, nil
}

func isTTY() bool {
	fd := int(os.Stdout.Fd())
	return term.IsTerminal(fd)
}
