# agentml-go

> **🚧 Early Alpha - Building in Public**
> 
> agentml-go is in early alpha and being built openly with the community. The vision is ambitious, the foundation is solid, but many features are still in development. Join us in shaping the future of agent standards.
>
> **📋 This Repository:** Contains Go implementations of AgentML namespace packages. These packages enable LLM integration, memory operations, I/O handling, and other capabilities for AgentML agents. For the language specification and runtime, see:
> - **[agentml](https://github.com/agentflare-ai/agentml)** - AgentML language specification and documentation
> - **[agentmlx](https://github.com/agentflare-ai/agentmlx)** - Reference runtime (Go/WASM) **NOT YET RELEASED**

---

## 📦 Available Namespaces

### LLM Integration

- **[gemini/](./gemini/)** - Google Gemini LLM integration with advanced features
  - Multi-model support (Flash, Pro, Thinking)
  - Streaming and structured generation
  - Rate limiting and complexity scoring
  - Tier-based model selection
  
- **[ollama/](./ollama/)** - Local LLM integration via Ollama
  - Run models locally
  - Full control over model selection
  - Privacy-first inference

### Memory & Storage

- **[memory/](./memory/)** - High-performance memory operations
  - Vector similarity search (powered by sqlite-vec)
  - Graph database with Cypher queries (powered by sqlite-graph)
  - Embedding generation
  - Persistent key-value storage
  - Everything in a single SQLite file

### I/O & Utilities

- **[stdin/](./stdin/)** - Standard input/output for console agents
- **[env/](./env/)** - Environment variable and configuration loading
- **[prompt/](./prompt/)** - Prompt management and snapshot utilities

## 🚀 Installation

Install individual packages as needed:

```bash
# Gemini namespace
go get github.com/agentflare-ai/agentml-go/gemini

# Ollama namespace
go get github.com/agentflare-ai/agentml-go/ollama

# Memory namespace
go get github.com/agentflare-ai/agentml-go/memory

# Or install all at once
go get github.com/agentflare-ai/agentml-go/...
```

## 📖 Usage

### In AgentML Files (.aml)

Reference these namespaces in your AgentML agent files:

```xml
<agent xmlns="github.com/agentflare-ai/agentml/agent"
       datamodel="ecmascript"
       import:gemini="github.com/agentflare-ai/agentml-go/gemini"
       import:memory="github.com/agentflare-ai/agentml-go/memory">

  <datamodel>
    <data id="user_input" expr="''" />
    <data id="embedding" expr="null" />
  </datamodel>

  <state id="process">
    <onentry>
      <!-- Generate embedding -->
      <memory:embed location="embedding" expr="user_input" />
      
      <!-- Query with Gemini -->
      <gemini:generate
        model="gemini-2.0-flash-exp"
        location="_event"
        promptexpr="'Analyze: ' + user_input" />
    </onentry>
    
    <transition event="response.ready" target="complete" />
  </state>
</agent>
```

### In Go Code

Import and use namespace packages directly in Go:

```go
package main

import (
    "context"
    "github.com/agentflare-ai/agentml-go/gemini"
    "github.com/agentflare-ai/agentml-go/memory"
)

func main() {
    ctx := context.Background()
    
    // Use Gemini client
    client, _ := gemini.NewClient(ctx, "YOUR_API_KEY")
    response, _ := client.Generate(ctx, "gemini-2.0-flash-exp", "Hello!")
    
    // Use memory operations
    db, _ := memory.Open("agent-memory.db")
    defer db.Close()
    
    // Store and search vectors
    embedding := []float32{0.1, 0.2, 0.3}
    db.StoreVector(ctx, "doc1", embedding, map[string]interface{}{
        "content": "Hello world",
    })
}
```

## 🏗️ Package Structure

Each namespace package includes:

```
namespace/
├── namespace.xsd        # XML Schema definition
├── namespace.go         # Namespace registration and core logic
├── executable.go        # Executable actions (AgentML runtime integration)
├── client.go            # Standalone client (optional, for direct Go usage)
├── *_test.go            # Tests
├── go.mod               # Go module definition
└── README.md            # Package-specific documentation
```

## 🔧 Development

### Prerequisites

- Go 1.24.5+
- Make (optional)
- Git with submodules support

### Setup

```bash
# Clone the repository
git clone --recurse-submodules https://github.com/agentflare-ai/agentml-go.git
cd agentml-go

# Install dependencies
go mod download

# Run tests
go test ./...

# Run tests for specific package
go test ./gemini/...
```

### Working with Go Workspace

This repository uses Go workspaces to manage multiple modules:

```bash
# Add new module to workspace
go work use ./new-namespace

# Sync workspace
go work sync
```

### Running Tests

```bash
# All tests
go test ./...

# With coverage
go test -cover ./...

# Specific package
go test -v ./gemini/...

# Integration tests (requires API keys)
GEMINI_API_KEY=your-key go test ./gemini/ -run Integration
```

## 📝 Creating Custom Namespaces

To create a new namespace:

1. **Create package directory**: `mkdir my-namespace`
2. **Add XSD schema**: Define your namespace schema in `my-namespace.xsd`
3. **Implement actions**: Create executable actions in `executable.go`
4. **Register namespace**: Implement `Register()` function in `namespace.go`
5. **Add tests**: Create comprehensive tests
6. **Document**: Write README.md with examples

See existing namespaces (gemini, ollama, memory) as reference implementations.

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines.

**Quick Start:**
1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## 📚 Documentation

- **[AgentML Specification](https://github.com/agentflare-ai/agentml)** - Core AgentML language spec
- **[agentmlx Runtime](https://github.com/agentflare-ai/agentmlx)** - Reference runtime implementation
- **[Enhancement Proposals (AEPs)](./aeps/)** - Propose major changes for agentml-go
- **[General AEPs](https://github.com/agentflare-ai/agentml/tree/main/aeps)** - Cross-project proposals

### Package-Specific Docs

- [Gemini Namespace](./gemini/README.md)
- [Ollama Namespace](./ollama/README.md)
- [Memory Namespace](./memory/README.md)

## 🔖 Versioning

This project follows [Semantic Versioning](https://semver.org/):

- **Major**: Breaking API changes
- **Minor**: New features, backward compatible
- **Patch**: Bug fixes, backward compatible

Releases are managed via [GitHub Releases](https://github.com/agentflare-ai/agentml-go/releases).

## 📄 License

MIT License - see [LICENSE](./LICENSE) for details.

Copyright (c) 2025 AgentFlare AI

## 🔗 Related Projects

- **[agentml](https://github.com/agentflare-ai/agentml)** - AgentML language specification
- **[agentmlx](https://github.com/agentflare-ai/agentmlx)** - Reference runtime (Go/WASM)
- **[sqlite-graph](https://github.com/agentflare-ai/sqlite-graph)** - Graph database extension for SQLite
- **[sqlite-vec](https://github.com/asg017/sqlite-vec)** - Vector search extension for SQLite

## 🆘 Support

- **Issues**: [Report bugs](https://github.com/agentflare-ai/agentml-go/issues)
- **Discussions**: [Ask questions](https://github.com/agentflare-ai/agentml/discussions)
- **Spec Issues**: [AgentML spec feedback](https://github.com/agentflare-ai/agentml/issues)

---

**Building the universal language for AI agents, one namespace at a time.** ✨

