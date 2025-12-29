package bubbletea

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/agentflare-ai/go-xmldom"
	"go.opentelemetry.io/otel/attribute"
)

type componentConfig interface {
	componentType() string
	componentID() string
	newAdapter(programID string) componentAdapter
	spanAttributes() []attribute.KeyValue
	events() componentEvents
}

type componentParser func(el xmldom.Element, displayName string) (componentConfig, error)

var (
	componentMu       sync.RWMutex
	componentRegistry = map[string]componentParser{}
)

func registerComponent(name string, parser componentParser) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" || parser == nil {
		return
	}
	componentMu.Lock()
	componentRegistry[key] = parser
	componentMu.Unlock()
}

func lookupComponent(name string) (componentParser, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil, false
	}
	componentMu.RLock()
	parser, ok := componentRegistry[key]
	componentMu.RUnlock()
	return parser, ok
}

func registeredComponentNames() []string {
	componentMu.RLock()
	defer componentMu.RUnlock()
	names := make([]string, 0, len(componentRegistry))
	for name := range componentRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func resolveComponentType(el xmldom.Element) (componentType string, displayName string, err error) {
	local := strings.ToLower(strings.TrimSpace(string(el.LocalName())))
	if local == "" {
		return "", "", fmt.Errorf("bubbletea: component element missing local name")
	}
	if local == "component" {
		return "", "", fmt.Errorf("bubbletea:component is not supported; use the concrete component element name")
	}
	return local, fmt.Sprintf("bubbletea:%s", local), nil
}
