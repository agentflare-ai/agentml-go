# MCP Namespace for AgentML

The MCP namespace provides Model Context Protocol (MCP) client functionality for AgentML agents, enabling them to connect to and interact with external MCP servers.

## Overview

The MCP namespace allows AgentML agents to:
- Connect to MCP servers via stdio or HTTP transports
- Call tools exposed by MCP servers
- Access resources from MCP servers
- Retrieve and use prompt templates
- List available capabilities

## Installation

```bash
go get github.com/agentflare-ai/agentml-go/mcp
```

## Usage in AgentML

### Namespace Declaration

```xml
<agentml xmlns="github.com/agentflare-ai/agentml"
       datamodel="ecmascript"
       xmlns:mcp="github.com/agentflare-ai/agentml-go/mcp">
```

### Connect to an MCP Server

#### Stdio Transport (Local Process)

```xml
<mcp:connect
  serverid="fs-server"
  command="mcp-server-filesystem"
  args="/path/to/workspace"
  location="connection_info" />
```

#### HTTP Transport (Remote Server)

```xml
<mcp:connect
  serverid="remote-server"
  transport="http"
  url="https://mcp-server.example.com"
  location="connection_info" />
```

### Call a Tool

```xml
<mcp:call
  serverid="fs-server"
  type="tool"
  name="read_file"
  paramsexpr='{"path": "config.json"}'
  location="file_content" />
```

The `type` attribute defaults to `"tool"`, so you can omit it:

```xml
<mcp:call
  serverid="fs-server"
  name="write_file"
  paramsexpr='{"path": "output.txt", "content": file_content}'
  location="write_result" />
```

### Get a Resource

```xml
<mcp:get
  serverid="fs-server"
  type="resource"
  uri="file:///path/to/file.txt"
  location="resource_data" />
```

### Get a Prompt Template

```xml
<mcp:get
  serverid="fs-server"
  type="prompt"
  name="analyze_code"
  argumentsexpr='{"language": "python", "focus": "performance"}'
  location="prompt_template" />
```

### List Available Items

List tools:

```xml
<mcp:list
  serverid="fs-server"
  type="tools"
  location="available_tools" />
```

List resources:

```xml
<mcp:list
  serverid="fs-server"
  type="resources"
  location="available_resources" />
```

List prompts:

```xml
<mcp:list
  serverid="fs-server"
  type="prompts"
  location="available_prompts" />
```

### Disconnect

```xml
<mcp:disconnect serverid="fs-server" />
```

## Complete Example

```xml
<agentml xmlns="github.com/agentflare-ai/agentml"
       datamodel="ecmascript"
       xmlns:mcp="github.com/agentflare-ai/agentml-go/mcp"
       xmlns:gemini="github.com/agentflare-ai/agentml-go/gemini">

  <datamodel>
    <data id="connection" expr="null" />
    <data id="tools" expr="null" />
    <data id="file_content" expr="null" />
    <data id="analysis" expr="null" />
  </datamodel>

  <state id="main">
    <!-- Connect to filesystem MCP server -->
    <state id="connect">
      <onentry>
        <mcp:connect
          serverid="fs-server"
          command="npx"
          args="-y @modelcontextprotocol/server-filesystem /tmp"
          location="connection" />
      </onentry>
      <transition target="list_tools" />
    </state>

    <!-- List available tools -->
    <state id="list_tools">
      <onentry>
        <mcp:list
          serverid="fs-server"
          type="tools"
          location="tools" />
        <log expr="'Available tools: ' + JSON.stringify(tools)" />
      </onentry>
      <transition target="read_file" />
    </state>

    <!-- Read a file using MCP tool -->
    <state id="read_file">
      <onentry>
        <mcp:call
          serverid="fs-server"
          name="read_file"
          paramsexpr='{"path": "/tmp/example.txt"}'
          location="file_content" />
        <log expr="'File content: ' + JSON.stringify(file_content)" />
      </onentry>
      <transition target="analyze" />
    </state>

    <!-- Analyze the file content with Gemini -->
    <state id="analyze">
      <onentry>
        <gemini:generate
          model="gemini-2.0-flash-exp"
          promptexpr="'Analyze this file content: ' + JSON.stringify(file_content)"
          location="_event" />
      </onentry>
      <transition event="response.ready" target="cleanup" />
    </state>

    <!-- Cleanup and disconnect -->
    <state id="cleanup">
      <onentry>
        <mcp:disconnect serverid="fs-server" />
      </onentry>
      <transition target="done" />
    </state>

    <final id="done" />
  </state>
</agentml>
```

## Using in Go Code

You can also use the MCP client directly in Go:

```go
package main

import (
    "context"
    "fmt"
    "github.com/agentflare-ai/agentml-go/mcp"
)

func main() {
    ctx := context.Background()

    // Create stdio client
    client, err := mcp.NewStdioClient(ctx, "npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp")
    if err != nil {
        panic(err)
    }
    defer client.Close()

    // List tools
    tools, err := client.ListTools(ctx)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Available tools: %+v\n", tools)

    // Call a tool
    result, err := client.CallTool(ctx, "read_file", map[string]interface{}{
        "path": "/tmp/example.txt",
    })
    if err != nil {
        panic(err)
    }
    fmt.Printf("Tool result: %+v\n", result)
}
```

## Elements Reference

### `<mcp:connect>`

Establishes a connection to an MCP server.

**Attributes:**
- `serverid` (required): Unique identifier for this connection
- `command`: Command to execute for stdio transport
- `commandexpr`: Expression that evaluates to the command
- `args`: Space-separated arguments for the command
- `argsexpr`: Expression that evaluates to the arguments
- `transport`: Transport type (`"stdio"` or `"http"`, defaults to `"stdio"`)
- `url`: URL for HTTP transport
- `urlexpr`: Expression that evaluates to the URL
- `location`: Data model location to store connection info

### `<mcp:call>`

Calls an MCP tool.

**Attributes:**
- `serverid` (required): Server connection identifier
- `type`: Call type (defaults to `"tool"`)
- `name`: Tool name
- `nameexpr`: Expression that evaluates to the tool name
- `params`: JSON string of parameters
- `paramsexpr`: Expression that evaluates to parameters object
- `location` (required): Data model location to store result

### `<mcp:get>`

Retrieves an MCP resource or prompt.

**Attributes:**
- `serverid` (required): Server connection identifier
- `type` (required): Type to get (`"resource"` or `"prompt"`)
- `name`: Name (for prompts)
- `nameexpr`: Expression that evaluates to the name
- `uri`: URI (for resources)
- `uriexpr`: Expression that evaluates to the URI
- `arguments`: JSON string of arguments (for prompts)
- `argumentsexpr`: Expression that evaluates to arguments object
- `location` (required): Data model location to store result

### `<mcp:list>`

Lists available items from an MCP server.

**Attributes:**
- `serverid` (required): Server connection identifier
- `type` (required): Type to list (`"tools"`, `"resources"`, or `"prompts"`)
- `location` (required): Data model location to store result

### `<mcp:disconnect>`

Closes a connection to an MCP server.

**Attributes:**
- `serverid` (required): Server connection identifier to close

## Common MCP Servers

- **@modelcontextprotocol/server-filesystem**: File system access
- **@modelcontextprotocol/server-github**: GitHub API access
- **@modelcontextprotocol/server-google-maps**: Google Maps integration
- **@modelcontextprotocol/server-brave-search**: Web search via Brave
- **@modelcontextprotocol/server-puppeteer**: Browser automation
- **@modelcontextprotocol/server-postgres**: PostgreSQL database access
- **@modelcontextprotocol/server-slack**: Slack workspace integration

See [MCP Servers](https://github.com/modelcontextprotocol/servers) for more.

## Error Handling

All MCP operations can fail and will raise `error.execution` platform events:

```xml
<state id="try_call_tool">
  <onentry>
    <mcp:call
      serverid="fs-server"
      name="read_file"
      paramsexpr='{"path": invalid_path}'
      location="result" />
  </onentry>
  <transition event="error.execution" target="handle_error">
    <log expr="'Tool call failed: ' + _event.data" />
  </transition>
  <transition target="success" />
</state>
```

## Protocol Details

The MCP namespace implements the [Model Context Protocol](https://modelcontextprotocol.io/) specification:
- Protocol Version: 2024-11-05
- JSON-RPC 2.0 message format
- Supports stdio and HTTP/SSE transports
- Full implementation of tools, resources, and prompts primitives

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for development guidelines.

## License

MIT License - see [LICENSE](../LICENSE) for details.
