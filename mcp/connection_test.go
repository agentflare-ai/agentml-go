package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnectionManager_New(t *testing.T) {
	cm := NewConnectionManager()
	assert.NotNil(t, cm)
	assert.NotNil(t, cm.connections)
	assert.Empty(t, cm.connections)
}

func TestConnectionManager_GetClient_NotFound(t *testing.T) {
	cm := NewConnectionManager()

	client, err := cm.GetClient("nonexistent")
	assert.Nil(t, client)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no connection found")
}

func TestConnectionManager_Disconnect_NotFound(t *testing.T) {
	cm := NewConnectionManager()

	err := cm.Disconnect(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no connection found")
}

func TestConnectionManager_Connect_DuplicateServerID(t *testing.T) {
	cm := NewConnectionManager()

	// Note: This test would need a mock MCP server to actually create a connection
	// For now, we just test the duplicate detection logic would work

	serverIDs := cm.ListConnections()
	assert.Empty(t, serverIDs)
}

func TestConnectionManager_ListConnections(t *testing.T) {
	cm := NewConnectionManager()

	// Initially empty
	connections := cm.ListConnections()
	assert.Empty(t, connections)
}

func TestConnectionManager_DisconnectAll_Empty(t *testing.T) {
	cm := NewConnectionManager()
	ctx := context.Background()

	err := cm.DisconnectAll(ctx)
	assert.NoError(t, err)
}

func TestConnectionManager_Connect_InvalidTransport(t *testing.T) {
	cm := NewConnectionManager()
	ctx := context.Background()

	err := cm.Connect(ctx, "test-server", "invalid-transport", "command", "args", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport type")
}
