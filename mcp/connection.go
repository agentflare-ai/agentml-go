package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ConnectionManager manages MCP client connections
type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*Client
}

// NewConnectionManager creates a new connection manager
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*Client),
	}
}

// Connect creates a new MCP connection with the given configuration
func (cm *ConnectionManager) Connect(ctx context.Context, serverID, transport, command, args, url string) error {
	ctx, span := tracer.Start(ctx, "mcp.ConnectionManager.Connect",
		trace.WithAttributes(
			attribute.String("server.id", serverID),
			attribute.String("transport", transport),
		))
	defer span.End()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if connection already exists
	if _, exists := cm.connections[serverID]; exists {
		return fmt.Errorf("connection with serverID '%s' already exists", serverID)
	}

	var client *Client
	var err error

	transportType := TransportType(strings.ToLower(transport))
	switch transportType {
	case TransportStdio, "":
		// Parse args if provided
		var argList []string
		if args != "" {
			argList = strings.Fields(args)
		}
		client, err = NewStdioClient(ctx, command, argList...)
	case TransportHTTP:
		client, err = NewHTTPClient(ctx, url)
	default:
		return fmt.Errorf("unsupported transport type: %s", transport)
	}

	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	cm.connections[serverID] = client
	return nil
}

// GetClient retrieves an existing MCP client by serverID
func (cm *ConnectionManager) GetClient(serverID string) (*Client, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	client, exists := cm.connections[serverID]
	if !exists {
		return nil, fmt.Errorf("no connection found with serverID '%s'", serverID)
	}

	return client, nil
}

// Disconnect closes and removes a connection
func (cm *ConnectionManager) Disconnect(ctx context.Context, serverID string) error {
	ctx, span := tracer.Start(ctx, "mcp.ConnectionManager.Disconnect",
		trace.WithAttributes(attribute.String("server.id", serverID)))
	defer span.End()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	client, exists := cm.connections[serverID]
	if !exists {
		return fmt.Errorf("no connection found with serverID '%s'", serverID)
	}

	if err := client.Close(); err != nil {
		return fmt.Errorf("failed to close connection: %w", err)
	}

	delete(cm.connections, serverID)
	return nil
}

// DisconnectAll closes all connections
func (cm *ConnectionManager) DisconnectAll(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "mcp.ConnectionManager.DisconnectAll")
	defer span.End()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	var errors []string
	for serverID, client := range cm.connections {
		if err := client.Close(); err != nil {
			errors = append(errors, fmt.Sprintf("serverID '%s': %v", serverID, err))
		}
	}

	cm.connections = make(map[string]*Client)

	if len(errors) > 0 {
		return fmt.Errorf("errors closing connections: %s", strings.Join(errors, "; "))
	}

	return nil
}

// ListConnections returns a list of all active connection serverIDs
func (cm *ConnectionManager) ListConnections() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	serverIDs := make([]string, 0, len(cm.connections))
	for serverID := range cm.connections {
		serverIDs = append(serverIDs, serverID)
	}

	return serverIDs
}
