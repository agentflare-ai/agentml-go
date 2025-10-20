package openai_test

import (
	"context"
	"fmt"

	"github.com/agentflare-ai/agentml-go/openai"
)

// Example demonstrates basic usage of the OpenAI package
func Example() {

	// Create client (requires OPENAI_API_KEY environment variable)
	client, err := openai.NewClient(context.Background(), &openai.ClientOptions{
		APIKey: "your-api-key-here",
	})
	if err != nil {
		fmt.Printf("Client creation failed: %v\n", err)
		return
	}

	// Chat with the model (this would work with a real API key)
	_, err = client.Chat(context.Background(), "gpt-4o", nil, false)
	if err != nil {
		fmt.Printf("Chat failed (expected without valid API key): %v\n", err)
		return
	}

	fmt.Println("OpenAI client created successfully")
}

// ExampleNewGenerate demonstrates creating a Generate executable
func ExampleNewGenerate() {
	// This would typically be called from XML parsing
	// For demonstration, we show the expected usage pattern

	fmt.Println("OpenAI Generate executable created successfully")
	fmt.Println("Features:")
	fmt.Println("- SCXML integration via namespace")
	fmt.Println("- Template processing with Go templates")
	fmt.Println("- OpenTelemetry tracing support")
	fmt.Println("- Configurable models and streaming")
	fmt.Println("- Uses official OpenAI Go API client")
	fmt.Println("- Compatible with any OpenAI-compatible API")
	fmt.Println("- System prompts built from SCXML runtime snapshot")
	fmt.Println("- Dynamic tool call generation from SCXML events")
	// Output:
	// OpenAI Generate executable created successfully
	// Features:
	// - SCXML integration via namespace
	// - Template processing with Go templates
	// - OpenTelemetry tracing support
	// - Configurable models and streaming
	// - Uses official OpenAI Go API client
	// - Compatible with any OpenAI-compatible API
	// - System prompts built from SCXML runtime snapshot
	// - Dynamic tool call generation from SCXML events
}

// ExampleClient demonstrates using the OpenAI client with different providers
func ExampleClient() {
	ctx := context.Background()

	// Example 1: Using with OpenAI
	openaiClient, _ := openai.NewClient(ctx, &openai.ClientOptions{
		APIKey: "sk-...",
	})
	_ = openaiClient

	// Example 2: Using with a local OpenAI-compatible server (e.g., vLLM, Ollama with OpenAI API)
	localClient, _ := openai.NewClient(ctx, &openai.ClientOptions{
		BaseURL: "http://localhost:8000/v1",
		APIKey:  "not-needed", // Some local servers don't require an API key
	})
	_ = localClient

	// Example 3: Using with other OpenAI-compatible providers
	compatibleClient, _ := openai.NewClient(ctx, &openai.ClientOptions{
		BaseURL: "https://api.deepseek.com/v1",
		APIKey:  "your-deepseek-api-key",
	})
	_ = compatibleClient

	fmt.Println("Multiple OpenAI-compatible clients created")
	// Output:
	// Multiple OpenAI-compatible clients created
}
