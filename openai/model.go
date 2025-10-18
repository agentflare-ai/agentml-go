package openai

// Model represents an OpenAI-compatible model configuration
type Model struct {
	Name   string
	Stream bool
}

// ModelName represents an OpenAI-compatible model name
type ModelName string

const (
	// OpenAI models
	GPT4         ModelName = "gpt-4"
	GPT4Turbo    ModelName = "gpt-4-turbo"
	GPT4o        ModelName = "gpt-4o"
	GPT4oMini    ModelName = "gpt-4o-mini"
	GPT35Turbo   ModelName = "gpt-3.5-turbo"
	O1           ModelName = "o1"
	O1Mini       ModelName = "o1-mini"
	O1Preview    ModelName = "o1-preview"

	// Common OpenAI-compatible models (e.g., from local providers, vLLM, etc.)
	DeepSeekCoder ModelName = "deepseek-coder"
	Llama3        ModelName = "llama-3"
	Mixtral       ModelName = "mixtral"
)

// NewModel creates a new OpenAI-compatible model configuration
func NewModel(name ModelName, stream bool) *Model {
	return &Model{
		Name:   string(name),
		Stream: stream,
	}
}
