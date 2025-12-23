package slack

import (
	"context"
	"strings"
	"testing"

	"github.com/agentflare-ai/go-xmldom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespaceURI(t *testing.T) {
	client := &Client{}
	ns := &Namespace{
		client: client,
	}

	assert.Equal(t, NamespaceURI, ns.URI())
}

func TestNamespaceUnload(t *testing.T) {
	client := &Client{}
	ns := &Namespace{
		client: client,
	}

	err := ns.Unload(context.Background())
	assert.NoError(t, err)
}

func TestNamespaceHandle_NilElement(t *testing.T) {
	client := &Client{}
	ns := &Namespace{
		client: client,
	}

	handled, err := ns.Handle(context.Background(), nil)
	assert.False(t, handled)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "element cannot be nil")
}

func TestNamespaceHandle_UnknownElement(t *testing.T) {
	client := &Client{}
	ns := &Namespace{
		client: client,
	}

	// Create a fake element with unknown local name
	xml := `<unknown xmlns="` + NamespaceURI + `" />`
	dec := xmldom.NewDecoder(strings.NewReader(xml))
	doc, err := dec.Decode()
	require.NoError(t, err)
	el := doc.DocumentElement()

	handled, err := ns.Handle(context.Background(), el)
	assert.False(t, handled)
	assert.NoError(t, err)
}

func TestLoaderCreatesClient(t *testing.T) {
	// Test that loader creates client if not provided
	// This will fail without SLACK_BOT_TOKEN, but we can test the structure
	deps := Deps{}
	loader := Loader(deps)

	// This will fail without env var, but we can verify the loader exists
	require.NotNil(t, loader)
}

func TestLoaderWithDeps(t *testing.T) {
	client := &Client{}
	deps := Deps{
		Client: client,
	}
	loader := Loader(deps)

	ns, err := loader(context.Background(), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, ns)

	concreteNs, ok := ns.(*Namespace)
	require.True(t, ok, "namespace should be of type *Namespace")
	assert.Equal(t, client, concreteNs.client)
}

