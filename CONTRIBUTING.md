# Contributing to agentml-go

Thank you for your interest in contributing to agentml-go! This repository contains the official Go implementations of AgentML namespace packages.

## Quick Links

- **[AgentML Specification](https://github.com/agentflare-ai/agentml)** - Main spec and documentation
- **[AgentML Contributing Guide](https://github.com/agentflare-ai/agentml/blob/main/CONTRIBUTING.md)** - General contributing guidelines
- **[agentmlx Runtime](https://github.com/agentflare-ai/agentmlx)** - Runtime implementation

## What to Contribute

### Namespace Improvements

- **Bug fixes** in existing namespaces (gemini, ollama, memory, etc.)
- **Performance optimizations**
- **New features** for existing namespaces
- **Tests** and test coverage improvements
- **Documentation** updates and examples

### New Namespaces

Want to add a new namespace? Great! Each namespace should:

1. Solve a clear use case for AgentML agents
2. Include XSD schema definition
3. Provide executable actions for runtime integration
4. Include comprehensive tests
5. Have clear documentation with examples

## Development Setup

### Prerequisites

- Go 1.24.5+
- Git with submodules support
- Make (optional)

### Clone and Setup

```bash
# Clone with submodules (for memory extensions)
git clone --recurse-submodules https://github.com/agentflare-ai/agentml-go.git
cd agentml-go

# If you already cloned without --recurse-submodules
git submodule update --init --recursive

# Install dependencies
go mod download
```

### Go Workspace

This repository uses Go workspaces to manage multiple modules. The workspace is already configured in `go.work`.

### Running Tests

```bash
# All tests
go test ./...

# Specific package
go test ./gemini/...

# With coverage
go test -cover ./...

# Integration tests (requires API keys)
GEMINI_API_KEY=your-key go test -v ./gemini/ -run Integration
```

### Code Quality

```bash
# Format code
go fmt ./...

# Vet code
go vet ./...

# Run linters (if golangci-lint installed)
golangci-lint run
```

## Contribution Process

### 1. Fork and Branch

```bash
# Fork on GitHub, then:
git clone https://github.com/YOUR_USERNAME/agentml-go.git
cd agentml-go

# Create feature branch
git checkout -b feature/your-feature-name
```

### 2. Make Changes

- Follow Go best practices and [Effective Go](https://golang.org/doc/effective_go.html)
- Write tests for new functionality
- Update documentation as needed
- Keep commits atomic and well-described

### 3. Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```bash
feat(gemini): add streaming support
fix(ollama): resolve timeout issue
docs(memory): update vector search examples
test(stdin): add integration tests
```

### 4. Test Your Changes

```bash
# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Check test coverage
go test -cover ./...
```

### 5. Submit Pull Request

- Push your branch to your fork
- Open a PR against `main` branch
- Fill out the PR template completely
- Link any related issues

## Coding Standards

### Go Code

```go
// ✅ Good: Clear, idiomatic Go
func ProcessEvent(ctx context.Context, event *Event) error {
    if event == nil {
        return fmt.Errorf("event cannot be nil")
    }
    
    // Process the event
    return nil
}

// ❌ Bad: Poor naming, no error handling
func pe(e interface{}) {
    // Process
}
```

**Guidelines:**
- Use `gofmt` for formatting
- Follow Go naming conventions
- Handle all errors explicitly
- Write clear comments explaining "why", not "what"
- Keep functions focused (single responsibility)
- Use context.Context for cancellation and timeouts

### Package Structure

```
namespace-name/
├── namespace.xsd        # XML Schema definition
├── namespace.go         # Registration and core types
├── executable.go        # AgentML executable actions
├── client.go            # Standalone client (optional)
├── *_test.go            # Tests
├── go.mod               # Module definition
└── README.md            # Documentation
```

### XSD Schema

Each namespace must include an XSD file defining its schema:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="github.com/agentflare-ai/agentml-go/yournamespace"
           elementFormDefault="qualified">
  
  <!-- Define your namespace elements -->
  <xs:element name="your-action">
    <xs:complexType>
      <xs:attribute name="param" type="xs:string" use="required"/>
      <xs:attribute name="location" type="xs:string"/>
    </xs:complexType>
  </xs:element>
</xs:schema>
```

## Testing Requirements

### Unit Tests

All new code must have unit tests:

```go
func TestYourFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
        wantErr  bool
    }{
        {"valid input", "test", "result", false},
        {"invalid input", "", "", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := YourFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("unexpected error: %v", err)
            }
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### Integration Tests

For tests requiring external services:

```go
func TestGeminiIntegration(t *testing.T) {
    apiKey := os.Getenv("GEMINI_API_KEY")
    if apiKey == "" {
        t.Skip("GEMINI_API_KEY not set")
    }
    
    // Test with real API
}
```

### Test Coverage

- Aim for 80%+ coverage for new code
- Test error paths and edge cases
- Include examples in documentation

## Documentation

### Package Documentation

Every package should have a README.md:

```markdown
# Namespace Name

Brief description of what this namespace does.

## Features

- Feature 1
- Feature 2

## Usage

```xml
<agentml import:ns="github.com/agentflare-ai/agentml-go/namespace">
  <ns:action param="value" />
</agentml>
```

## Configuration

...

## Examples

...
```

### Code Comments

```go
// Package gemini provides AgentML integration with Google Gemini LLM.
// It supports multiple models, streaming, and structured generation.
package gemini

// GenerateAction executes LLM generation with the specified model and prompt.
// It stores the result in the location specified by the Location attribute.
type GenerateAction struct {
    Model    string `xml:"model,attr"`
    Location string `xml:"location,attr"`
    Prompt   string `xml:"prompt,attr"`
}
```

## Release Process

This repository uses semantic versioning and GitHub Releases:

- **Patch** (0.0.x): Bug fixes, minor improvements
- **Minor** (0.x.0): New features, backward compatible
- **Major** (x.0.0): Breaking changes

Releases are automated via GoReleaser.

## Getting Help

- **Documentation**: Check namespace READMEs
- **Issues**: [Report bugs](https://github.com/agentflare-ai/agentml-go/issues)
- **Discussions**: [Ask questions](https://github.com/agentflare-ai/agentml/discussions)
- **Spec Questions**: [AgentML spec issues](https://github.com/agentflare-ai/agentml/issues)

## Code of Conduct

Be respectful, inclusive, and constructive. We're building this together.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

---

**Thank you for helping build the Go implementation of AgentML namespaces!** 🚀

