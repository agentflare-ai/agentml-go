package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONRPCRequest_Structure(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test/method",
		Params:  map[string]interface{}{"key": "value"},
	}

	assert.Equal(t, "2.0", req.JSONRPC)
	assert.Equal(t, int64(1), req.ID)
	assert.Equal(t, "test/method", req.Method)
	assert.NotNil(t, req.Params)
}

func TestJSONRPCError_Structure(t *testing.T) {
	err := JSONRPCError{
		Code:    -32600,
		Message: "Invalid Request",
	}

	assert.Equal(t, -32600, err.Code)
	assert.Equal(t, "Invalid Request", err.Message)
}

func TestTransportType_Constants(t *testing.T) {
	assert.Equal(t, TransportType("stdio"), TransportStdio)
	assert.Equal(t, TransportType("http"), TransportHTTP)
}

func TestTool_Structure(t *testing.T) {
	tool := Tool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"param": map[string]interface{}{
					"type": "string",
				},
			},
		},
	}

	assert.Equal(t, "test_tool", tool.Name)
	assert.Equal(t, "A test tool", tool.Description)
	assert.NotNil(t, tool.InputSchema)
}

func TestResource_Structure(t *testing.T) {
	resource := Resource{
		URI:         "file:///path/to/file.txt",
		Name:        "file.txt",
		Description: "A test file",
		MimeType:    "text/plain",
	}

	assert.Equal(t, "file:///path/to/file.txt", resource.URI)
	assert.Equal(t, "file.txt", resource.Name)
	assert.Equal(t, "text/plain", resource.MimeType)
}

func TestPrompt_Structure(t *testing.T) {
	prompt := Prompt{
		Name:        "test_prompt",
		Description: "A test prompt",
		Arguments: []PromptArgument{
			{
				Name:        "arg1",
				Description: "First argument",
				Required:    true,
			},
		},
	}

	assert.Equal(t, "test_prompt", prompt.Name)
	assert.Equal(t, "A test prompt", prompt.Description)
	assert.Len(t, prompt.Arguments, 1)
	assert.True(t, prompt.Arguments[0].Required)
}

func TestContent_Structure(t *testing.T) {
	content := Content{
		Type:     "text",
		Text:     "Hello, world!",
		MimeType: "text/plain",
	}

	assert.Equal(t, "text", content.Type)
	assert.Equal(t, "Hello, world!", content.Text)
	assert.Equal(t, "text/plain", content.MimeType)
}
