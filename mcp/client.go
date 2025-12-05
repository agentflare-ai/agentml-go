package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/agentflare-ai/agentml-go/mcp")

// TransportType defines the transport mechanism for MCP communication
type TransportType string

const (
	TransportStdio TransportType = "stdio"
	TransportHTTP  TransportType = "http"
)

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Client represents an MCP client that can connect to MCP servers
type Client struct {
	transport TransportType
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	httpURL   string
	mu        sync.Mutex
	nextID    atomic.Int64
	reader    *bufio.Reader
}

// NewStdioClient creates a new MCP client using stdio transport
func NewStdioClient(ctx context.Context, command string, args ...string) (*Client, error) {
	ctx, span := tracer.Start(ctx, "mcp.NewStdioClient",
		trace.WithAttributes(
			attribute.String("command", command),
			attribute.StringSlice("args", args),
		))
	defer span.End()

	cmd := exec.CommandContext(ctx, command, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start MCP server process: %w", err)
	}

	client := &Client{
		transport: TransportStdio,
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		reader:    bufio.NewReader(stdout),
	}

	// Initialize the MCP connection
	if err := client.initialize(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	return client, nil
}

// NewHTTPClient creates a new MCP client using HTTP transport
func NewHTTPClient(ctx context.Context, url string) (*Client, error) {
	ctx, span := tracer.Start(ctx, "mcp.NewHTTPClient",
		trace.WithAttributes(attribute.String("url", url)))
	defer span.End()

	client := &Client{
		transport: TransportHTTP,
		httpURL:   url,
	}

	// Initialize the MCP connection
	if err := client.initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize MCP connection: %w", err)
	}

	return client, nil
}

// initialize sends the MCP initialize request and handles the response
func (c *Client) initialize(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "mcp.Client.initialize")
	defer span.End()

	initParams := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"roots": map[string]interface{}{
				"listChanged": false,
			},
		},
		"clientInfo": map[string]interface{}{
			"name":    "agentml-go-mcp",
			"version": "0.1.0",
		},
	}

	result, err := c.Call(ctx, "initialize", initParams)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}

	// Parse initialize response to verify server capabilities
	var initResponse struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools     map[string]interface{} `json:"tools,omitempty"`
			Resources map[string]interface{} `json:"resources,omitempty"`
			Prompts   map[string]interface{} `json:"prompts,omitempty"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}

	if err := json.Unmarshal(result, &initResponse); err != nil {
		return fmt.Errorf("failed to parse initialize response: %w", err)
	}

	span.SetAttributes(
		attribute.String("server.name", initResponse.ServerInfo.Name),
		attribute.String("server.version", initResponse.ServerInfo.Version),
		attribute.String("protocol.version", initResponse.ProtocolVersion),
	)

	// Send initialized notification
	if err := c.Notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("initialized notification failed: %w", err)
	}

	return nil
}

// Call sends a JSON-RPC request and waits for the response
func (c *Client) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	ctx, span := tracer.Start(ctx, "mcp.Client.Call",
		trace.WithAttributes(attribute.String("method", method)))
	defer span.End()

	c.mu.Lock()
	defer c.mu.Unlock()

	requestID := c.nextID.Add(1)

	request := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      requestID,
		Method:  method,
		Params:  params,
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	switch c.transport {
	case TransportStdio:
		return c.callStdio(ctx, requestBytes)
	case TransportHTTP:
		return c.callHTTP(ctx, requestBytes)
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", c.transport)
	}
}

// callStdio sends a request over stdio and reads the response
func (c *Client) callStdio(ctx context.Context, requestBytes []byte) (json.RawMessage, error) {
	// Write request with newline delimiter
	if _, err := c.stdin.Write(append(requestBytes, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response line
	responseBytes, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response JSONRPCResponse
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", response.Error.Code, response.Error.Message)
	}

	return response.Result, nil
}

// callHTTP sends a request over HTTP and reads the response
func (c *Client) callHTTP(ctx context.Context, requestBytes []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.httpURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(io.MultiReader(
		io.NopCloser(io.Reader(nil)),
	))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
	}

	var response JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", response.Error.Code, response.Error.Message)
	}

	return response.Result, nil
}

// Notify sends a JSON-RPC notification (no response expected)
func (c *Client) Notify(ctx context.Context, method string, params interface{}) error {
	ctx, span := tracer.Start(ctx, "mcp.Client.Notify",
		trace.WithAttributes(attribute.String("method", method)))
	defer span.End()

	c.mu.Lock()
	defer c.mu.Unlock()

	notification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}

	notificationBytes, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	switch c.transport {
	case TransportStdio:
		if _, err := c.stdin.Write(append(notificationBytes, '\n')); err != nil {
			return fmt.Errorf("failed to write notification: %w", err)
		}
	case TransportHTTP:
		req, err := http.NewRequestWithContext(ctx, "POST", c.httpURL, io.NopCloser(io.Reader(nil)))
		if err != nil {
			return fmt.Errorf("failed to create HTTP request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("HTTP request failed: %w", err)
		}
		resp.Body.Close()
	}

	return nil
}

// ListTools requests the list of available tools from the MCP server
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	ctx, span := tracer.Start(ctx, "mcp.Client.ListTools")
	defer span.End()

	result, err := c.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to parse tools/list response: %w", err)
	}

	return response.Tools, nil
}

// CallTool invokes a tool on the MCP server
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]interface{}) ([]Content, error) {
	ctx, span := tracer.Start(ctx, "mcp.Client.CallTool",
		trace.WithAttributes(attribute.String("tool.name", name)))
	defer span.End()

	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}

	result, err := c.Call(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}

	var response struct {
		Content []Content `json:"content"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to parse tools/call response: %w", err)
	}

	return response.Content, nil
}

// ListResources requests the list of available resources from the MCP server
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	ctx, span := tracer.Start(ctx, "mcp.Client.ListResources")
	defer span.End()

	result, err := c.Call(ctx, "resources/list", nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to parse resources/list response: %w", err)
	}

	return response.Resources, nil
}

// ReadResource reads a resource from the MCP server
func (c *Client) ReadResource(ctx context.Context, uri string) ([]Content, error) {
	ctx, span := tracer.Start(ctx, "mcp.Client.ReadResource",
		trace.WithAttributes(attribute.String("resource.uri", uri)))
	defer span.End()

	params := map[string]interface{}{
		"uri": uri,
	}

	result, err := c.Call(ctx, "resources/read", params)
	if err != nil {
		return nil, err
	}

	var response struct {
		Contents []Content `json:"contents"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to parse resources/read response: %w", err)
	}

	return response.Contents, nil
}

// ListPrompts requests the list of available prompts from the MCP server
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	ctx, span := tracer.Start(ctx, "mcp.Client.ListPrompts")
	defer span.End()

	result, err := c.Call(ctx, "prompts/list", nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to parse prompts/list response: %w", err)
	}

	return response.Prompts, nil
}

// GetPrompt retrieves a prompt from the MCP server
func (c *Client) GetPrompt(ctx context.Context, name string, arguments map[string]string) (*PromptMessage, error) {
	ctx, span := tracer.Start(ctx, "mcp.Client.GetPrompt",
		trace.WithAttributes(attribute.String("prompt.name", name)))
	defer span.End()

	params := map[string]interface{}{
		"name":      name,
		"arguments": arguments,
	}

	result, err := c.Call(ctx, "prompts/get", params)
	if err != nil {
		return nil, err
	}

	var response PromptMessage
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to parse prompts/get response: %w", err)
	}

	return &response, nil
}

// Close closes the MCP client and cleans up resources
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.transport == TransportStdio {
		if c.stdin != nil {
			c.stdin.Close()
		}
		if c.stdout != nil {
			c.stdout.Close()
		}
		if c.stderr != nil {
			c.stderr.Close()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			c.cmd.Process.Kill()
			c.cmd.Wait()
		}
	}

	return nil
}

// Tool represents an MCP tool
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// Resource represents an MCP resource
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Prompt represents an MCP prompt template
type Prompt struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Arguments   []PromptArgument  `json:"arguments,omitempty"`
}

// PromptArgument represents an argument for a prompt
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage represents a prompt message with its content
type PromptMessage struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptContent `json:"messages"`
}

// PromptContent represents the content of a prompt message
type PromptContent struct {
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

// Content represents content returned by MCP operations
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}
