package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespaceURI(t *testing.T) {
	deps := &Deps{
		ConnectionManager: NewConnectionManager(),
	}
	loader := Loader(deps)

	ns, err := loader(context.Background(), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, ns)

	assert.Equal(t, MCPNamespaceURI, ns.URI())
}

func TestNamespaceHandle_InvalidElement(t *testing.T) {
	deps := &Deps{
		ConnectionManager: NewConnectionManager(),
	}
	loader := Loader(deps)

	ns, err := loader(context.Background(), nil, nil)
	require.NoError(t, err)

	// Test with nil element
	handled, err := ns.Handle(context.Background(), nil)
	assert.False(t, handled)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "element cannot be nil")
}

func TestNamespaceUnload(t *testing.T) {
	deps := &Deps{
		ConnectionManager: NewConnectionManager(),
	}
	loader := Loader(deps)

	ns, err := loader(context.Background(), nil, nil)
	require.NoError(t, err)

	// Should not error even with no connections
	err = ns.Unload(context.Background())
	assert.NoError(t, err)
}

func TestLoaderCreatesConnectionManager(t *testing.T) {
	// Test that loader creates connection manager if not provided
	loader := Loader(nil)

	namespace, err := loader(context.Background(), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, namespace)

	// Should have a connection manager created internally
	concreteNs, ok := namespace.(*ns)
	require.True(t, ok, "namespace should be of type *ns")
	assert.NotNil(t, concreteNs.deps.ConnectionManager)
}
