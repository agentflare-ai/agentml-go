# @agentml-go/openai

OpenAI-compatible LLM integration for AgentML. This package provides SCXML executable elements for AI generation using OpenAI and any OpenAI-compatible API.

## Features

- **OpenAI API Integration**: Uses the official [openai-go](https://github.com/openai/openai-go) client
- **OpenAI-Compatible**: Works with any OpenAI-compatible API (vLLM, LocalAI, Ollama with OpenAI API, etc.)
- **SCXML Integration**: Seamless integration with AgentML state machines via namespace
- **Dynamic System Prompts**: Automatically builds system prompts from SCXML runtime snapshots
- **Tool Call Generation**: Dynamically generates and processes `send_*` tool calls for SCXML events
- **Template Support**: Go template processing for dynamic prompts
- **OpenTelemetry**: Built-in tracing support for observability
- **Streaming Support**: Configurable streaming for real-time responses (coming soon)

## Installation

```bash
go get github.com/agentflare-ai/agentml-go/openai
```

## Usage

### Basic Usage

```go
package main

import (
    "context"
    "github.com/agentflare-ai/agentml-go/openai"
)

func main() {
    ctx := context.Background()

    // Create models
    models := map[openai.ModelName]*openai.Model{
        "gpt-4o": openai.NewModel(openai.GPT4o, false),
    }

    // Create client
    client, err := openai.NewClient(ctx, models, &openai.ClientOptions{
        APIKey: "your-api-key-here",
    })
    if err != nil {
        panic(err)
    }

    // Use with AgentML
    deps := &openai.Deps{Client: client}
    // Register namespace with interpreter...
}
```

### Using with Different Providers

#### OpenAI
```go
client, _ := openai.NewClient(ctx, models, &openai.ClientOptions{
    APIKey: "sk-...",
})
```

#### Local OpenAI-Compatible Server (vLLM, LocalAI, etc.)
```go
client, _ := openai.NewClient(ctx, models, &openai.ClientOptions{
    BaseURL: "http://localhost:8000/v1",
    APIKey:  "not-needed", // Some local servers don't require an API key
})
```

#### Ollama with OpenAI API
```go
client, _ := openai.NewClient(ctx, models, &openai.ClientOptions{
    BaseURL: "http://localhost:11434/v1",
    APIKey:  "ollama", // Ollama doesn't require a real API key
})
```

### SCXML XML Usage

```xml
<scxml xmlns="http://www.w3.org/2005/07/scxml"
       xmlns:openai="github.com/agentflare-ai/agentml/openai"
       version="1.0">

  <state id="main">
    <onentry>
      <openai:generate
          model="gpt-4o"
          prompt="What is the meaning of life?"
          location="response" />
    </onentry>
  </state>
</scxml>
```

### Dynamic Prompts

```xml
<openai:generate model="gpt-4o" location="response">
  <openai:prompt>
    You are a helpful assistant.
    Current state: {{.currentState}}
    User input: {{.userInput}}
  </openai:prompt>
</openai:generate>
```

### Namespace Registration

```go
import (
    "github.com/agentflare-ai/agentml"
    "github.com/agentflare-ai/agentml-go/openai"
)

// Create OpenAI client and deps
deps := &openai.Deps{Client: client}

// Register namespace with interpreter
interpreter.RegisterNamespace(openai.Loader(deps))
```

## How It Works

### System Prompts from Runtime Snapshots

The OpenAI package automatically builds system prompts from the SCXML runtime snapshot. This gives the LLM context about:
- Current state machine configuration
- Active states
- Available transitions
- Data model state (if included)

### Dynamic Tool Calls

The package generates tool definitions dynamically from available SCXML events. When the LLM calls a `send_*` function, it automatically:
1. Parses the function name to extract the event name
2. Extracts event data from function arguments
3. Sends the corresponding SCXML event via `interpreter.Send()`

This enables the LLM to drive state machine transitions directly.

## Supported Models

### OpenAI Models
- `gpt-4`
- `gpt-4-turbo`
- `gpt-4o`
- `gpt-4o-mini`
- `gpt-3.5-turbo`
- `o1`, `o1-mini`, `o1-preview`

### Custom Models
You can use any model name supported by your OpenAI-compatible provider:

```go
models := map[openai.ModelName]*openai.Model{
    openai.ModelName("custom-model"): openai.NewModel(openai.ModelName("custom-model"), false),
}
```

## API Reference

See the [GoDoc](https://pkg.go.dev/github.com/agentflare-ai/agentml-go/openai) for detailed API documentation.

## Related Packages

- [@agentml-go/ollama](../ollama) - Ollama-specific integration with native API
- [@agentml-go/gemini](../gemini) - Google Gemini integration

## License

See the main repository LICENSE file.
