package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/agentflare-ai/agentml-go"
	"github.com/agentflare-ai/go-xmldom"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Connect represents an MCP connect executable element
type Connect struct {
	xmldom.Element
	ServerID     string
	Command      string
	CommandExpr  string
	Args         string
	ArgsExpr     string
	Transport    string
	URL          string
	URLExpr      string
	Location     string
	connMgr      *ConnectionManager
}

// NewConnect creates a new Connect executable from an XML element
func NewConnect(ctx context.Context, el xmldom.Element, connMgr *ConnectionManager) (*Connect, error) {
	return &Connect{
		Element:      el,
		ServerID:     string(el.GetAttribute("serverid")),
		Command:      string(el.GetAttribute("command")),
		CommandExpr:  string(el.GetAttribute("commandexpr")),
		Args:         string(el.GetAttribute("args")),
		ArgsExpr:     string(el.GetAttribute("argsexpr")),
		Transport:    string(el.GetAttribute("transport")),
		URL:          string(el.GetAttribute("url")),
		URLExpr:      string(el.GetAttribute("urlexpr")),
		Location:     string(el.GetAttribute("location")),
		connMgr:      connMgr,
	}, nil
}

// Execute implements the agentml.Executor interface for Connect
func (c *Connect) Execute(ctx context.Context, interpreter agentml.Interpreter) error {
	ctx, span := tracer.Start(ctx, "mcp.Connect.Execute",
		trace.WithAttributes(attribute.String("server.id", c.ServerID)))
	defer span.End()

	if c.ServerID == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:connect requires 'serverid' attribute",
		}
	}

	// Evaluate dynamic expressions
	command := c.Command
	if c.CommandExpr != "" {
		val, err := interpreter.DataModel().EvaluateValue(ctx, c.CommandExpr)
		if err != nil {
			return fmt.Errorf("failed to evaluate commandexpr: %w", err)
		}
		command = fmt.Sprintf("%v", val)
	}

	args := c.Args
	if c.ArgsExpr != "" {
		val, err := interpreter.DataModel().EvaluateValue(ctx, c.ArgsExpr)
		if err != nil {
			return fmt.Errorf("failed to evaluate argsexpr: %w", err)
		}
		args = fmt.Sprintf("%v", val)
	}

	url := c.URL
	if c.URLExpr != "" {
		val, err := interpreter.DataModel().EvaluateValue(ctx, c.URLExpr)
		if err != nil {
			return fmt.Errorf("failed to evaluate urlexpr: %w", err)
		}
		url = fmt.Sprintf("%v", val)
	}

	transport := c.Transport
	if transport == "" {
		transport = "stdio"
	}

	// Connect to MCP server
	if err := c.connMgr.Connect(ctx, c.ServerID, transport, command, args, url); err != nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("failed to connect to MCP server: %v", err),
		}
	}

	// Store connection info in location if specified
	if c.Location != "" {
		connectionInfo := map[string]interface{}{
			"serverid":  c.ServerID,
			"transport": transport,
			"connected": true,
		}
		if err := interpreter.DataModel().Assign(ctx, c.Location, connectionInfo); err != nil {
			return fmt.Errorf("failed to assign connection info to location: %w", err)
		}
	}

	return nil
}

// Call represents an MCP call executable element
type Call struct {
	xmldom.Element
	ServerID   string
	Type       string
	Name       string
	NameExpr   string
	Params     string
	ParamsExpr string
	Location   string
	connMgr    *ConnectionManager
}

// NewCall creates a new Call executable from an XML element
func NewCall(ctx context.Context, el xmldom.Element, connMgr *ConnectionManager) (*Call, error) {
	callType := string(el.GetAttribute("type"))
	if callType == "" {
		callType = "tool"
	}

	return &Call{
		Element:    el,
		ServerID:   string(el.GetAttribute("serverid")),
		Type:       callType,
		Name:       string(el.GetAttribute("name")),
		NameExpr:   string(el.GetAttribute("nameexpr")),
		Params:     string(el.GetAttribute("params")),
		ParamsExpr: string(el.GetAttribute("paramsexpr")),
		Location:   string(el.GetAttribute("location")),
		connMgr:    connMgr,
	}, nil
}

// Execute implements the agentml.Executor interface for Call
func (c *Call) Execute(ctx context.Context, interpreter agentml.Interpreter) error {
	ctx, span := tracer.Start(ctx, "mcp.Call.Execute",
		trace.WithAttributes(
			attribute.String("server.id", c.ServerID),
			attribute.String("type", c.Type),
		))
	defer span.End()

	if c.ServerID == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:call requires 'serverid' attribute",
		}
	}

	if c.Location == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:call requires 'location' attribute",
		}
	}

	// Get client
	client, err := c.connMgr.GetClient(c.ServerID)
	if err != nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("failed to get MCP client: %v", err),
		}
	}

	// Evaluate name
	name := c.Name
	if c.NameExpr != "" {
		val, err := interpreter.DataModel().EvaluateValue(ctx, c.NameExpr)
		if err != nil {
			return fmt.Errorf("failed to evaluate nameexpr: %w", err)
		}
		name = fmt.Sprintf("%v", val)
	}

	if name == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:call requires 'name' or 'nameexpr' attribute",
		}
	}

	// Evaluate params
	var params map[string]interface{}
	if c.ParamsExpr != "" {
		val, err := interpreter.DataModel().EvaluateValue(ctx, c.ParamsExpr)
		if err != nil {
			return fmt.Errorf("failed to evaluate paramsexpr: %w", err)
		}
		// Convert to map
		if m, ok := val.(map[string]interface{}); ok {
			params = m
		} else {
			// Try to marshal/unmarshal through JSON
			jsonBytes, err := json.Marshal(val)
			if err != nil {
				return fmt.Errorf("failed to marshal params: %w", err)
			}
			if err := json.Unmarshal(jsonBytes, &params); err != nil {
				return fmt.Errorf("failed to unmarshal params: %w", err)
			}
		}
	} else if c.Params != "" {
		if err := json.Unmarshal([]byte(c.Params), &params); err != nil {
			return fmt.Errorf("failed to parse params JSON: %w", err)
		}
	}

	// Execute call based on type
	var result interface{}
	switch c.Type {
	case "tool":
		contents, err := client.CallTool(ctx, name, params)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("failed to call tool: %v", err),
			}
		}
		result = contents
	default:
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("unsupported call type: %s", c.Type),
		}
	}

	// Store result
	if err := interpreter.DataModel().Assign(ctx, c.Location, result); err != nil {
		return fmt.Errorf("failed to assign result to location: %w", err)
	}

	return nil
}

// Get represents an MCP get executable element
type Get struct {
	xmldom.Element
	ServerID      string
	Type          string
	Name          string
	NameExpr      string
	URI           string
	URIExpr       string
	Arguments     string
	ArgumentsExpr string
	Location      string
	connMgr       *ConnectionManager
}

// NewGet creates a new Get executable from an XML element
func NewGet(ctx context.Context, el xmldom.Element, connMgr *ConnectionManager) (*Get, error) {
	return &Get{
		Element:       el,
		ServerID:      string(el.GetAttribute("serverid")),
		Type:          string(el.GetAttribute("type")),
		Name:          string(el.GetAttribute("name")),
		NameExpr:      string(el.GetAttribute("nameexpr")),
		URI:           string(el.GetAttribute("uri")),
		URIExpr:       string(el.GetAttribute("uriexpr")),
		Arguments:     string(el.GetAttribute("arguments")),
		ArgumentsExpr: string(el.GetAttribute("argumentsexpr")),
		Location:      string(el.GetAttribute("location")),
		connMgr:       connMgr,
	}, nil
}

// Execute implements the agentml.Executor interface for Get
func (g *Get) Execute(ctx context.Context, interpreter agentml.Interpreter) error {
	ctx, span := tracer.Start(ctx, "mcp.Get.Execute",
		trace.WithAttributes(
			attribute.String("server.id", g.ServerID),
			attribute.String("type", g.Type),
		))
	defer span.End()

	if g.ServerID == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:get requires 'serverid' attribute",
		}
	}

	if g.Type == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:get requires 'type' attribute",
		}
	}

	if g.Location == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:get requires 'location' attribute",
		}
	}

	// Get client
	client, err := g.connMgr.GetClient(g.ServerID)
	if err != nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("failed to get MCP client: %v", err),
		}
	}

	var result interface{}

	switch g.Type {
	case "resource":
		// Evaluate URI
		uri := g.URI
		if g.URIExpr != "" {
			val, err := interpreter.DataModel().EvaluateValue(ctx, g.URIExpr)
			if err != nil {
				return fmt.Errorf("failed to evaluate uriexpr: %w", err)
			}
			uri = fmt.Sprintf("%v", val)
		}

		if uri == "" {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   "mcp:get type='resource' requires 'uri' or 'uriexpr' attribute",
			}
		}

		contents, err := client.ReadResource(ctx, uri)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("failed to read resource: %v", err),
			}
		}
		result = contents

	case "prompt":
		// Evaluate name
		name := g.Name
		if g.NameExpr != "" {
			val, err := interpreter.DataModel().EvaluateValue(ctx, g.NameExpr)
			if err != nil {
				return fmt.Errorf("failed to evaluate nameexpr: %w", err)
			}
			name = fmt.Sprintf("%v", val)
		}

		if name == "" {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   "mcp:get type='prompt' requires 'name' or 'nameexpr' attribute",
			}
		}

		// Evaluate arguments
		var arguments map[string]string
		if g.ArgumentsExpr != "" {
			val, err := interpreter.DataModel().EvaluateValue(ctx, g.ArgumentsExpr)
			if err != nil {
				return fmt.Errorf("failed to evaluate argumentsexpr: %w", err)
			}
			// Convert to map[string]string
			if m, ok := val.(map[string]string); ok {
				arguments = m
			} else if m, ok := val.(map[string]interface{}); ok {
				arguments = make(map[string]string)
				for k, v := range m {
					arguments[k] = fmt.Sprintf("%v", v)
				}
			}
		} else if g.Arguments != "" {
			if err := json.Unmarshal([]byte(g.Arguments), &arguments); err != nil {
				return fmt.Errorf("failed to parse arguments JSON: %w", err)
			}
		}

		promptMsg, err := client.GetPrompt(ctx, name, arguments)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("failed to get prompt: %v", err),
			}
		}
		result = promptMsg

	default:
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("unsupported get type: %s", g.Type),
		}
	}

	// Store result
	if err := interpreter.DataModel().Assign(ctx, g.Location, result); err != nil {
		return fmt.Errorf("failed to assign result to location: %w", err)
	}

	return nil
}

// List represents an MCP list executable element
type List struct {
	xmldom.Element
	ServerID string
	Type     string
	Location string
	connMgr  *ConnectionManager
}

// NewList creates a new List executable from an XML element
func NewList(ctx context.Context, el xmldom.Element, connMgr *ConnectionManager) (*List, error) {
	return &List{
		Element:  el,
		ServerID: string(el.GetAttribute("serverid")),
		Type:     string(el.GetAttribute("type")),
		Location: string(el.GetAttribute("location")),
		connMgr:  connMgr,
	}, nil
}

// Execute implements the agentml.Executor interface for List
func (l *List) Execute(ctx context.Context, interpreter agentml.Interpreter) error {
	ctx, span := tracer.Start(ctx, "mcp.List.Execute",
		trace.WithAttributes(
			attribute.String("server.id", l.ServerID),
			attribute.String("type", l.Type),
		))
	defer span.End()

	if l.ServerID == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:list requires 'serverid' attribute",
		}
	}

	if l.Type == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:list requires 'type' attribute",
		}
	}

	if l.Location == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:list requires 'location' attribute",
		}
	}

	// Get client
	client, err := l.connMgr.GetClient(l.ServerID)
	if err != nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("failed to get MCP client: %v", err),
		}
	}

	var result interface{}

	switch l.Type {
	case "tools":
		tools, err := client.ListTools(ctx)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("failed to list tools: %v", err),
			}
		}
		result = tools

	case "resources":
		resources, err := client.ListResources(ctx)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("failed to list resources: %v", err),
			}
		}
		result = resources

	case "prompts":
		prompts, err := client.ListPrompts(ctx)
		if err != nil {
			return &agentml.PlatformError{
				EventName: "error.execution",
				Message:   fmt.Sprintf("failed to list prompts: %v", err),
			}
		}
		result = prompts

	default:
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("unsupported list type: %s", l.Type),
		}
	}

	// Store result
	if err := interpreter.DataModel().Assign(ctx, l.Location, result); err != nil {
		return fmt.Errorf("failed to assign result to location: %w", err)
	}

	return nil
}

// Disconnect represents an MCP disconnect executable element
type Disconnect struct {
	xmldom.Element
	ServerID string
	connMgr  *ConnectionManager
}

// NewDisconnect creates a new Disconnect executable from an XML element
func NewDisconnect(ctx context.Context, el xmldom.Element, connMgr *ConnectionManager) (*Disconnect, error) {
	return &Disconnect{
		Element:  el,
		ServerID: string(el.GetAttribute("serverid")),
		connMgr:  connMgr,
	}, nil
}

// Execute implements the agentml.Executor interface for Disconnect
func (d *Disconnect) Execute(ctx context.Context, interpreter agentml.Interpreter) error {
	ctx, span := tracer.Start(ctx, "mcp.Disconnect.Execute",
		trace.WithAttributes(attribute.String("server.id", d.ServerID)))
	defer span.End()

	if d.ServerID == "" {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   "mcp:disconnect requires 'serverid' attribute",
		}
	}

	if err := d.connMgr.Disconnect(ctx, d.ServerID); err != nil {
		return &agentml.PlatformError{
			EventName: "error.execution",
			Message:   fmt.Sprintf("failed to disconnect: %v", err),
		}
	}

	return nil
}
