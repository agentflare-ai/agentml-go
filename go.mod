module github.com/agentflare-ai/agentml-go

go 1.24.5

require github.com/agentflare-ai/go-xmldom v0.1.0

require (
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	golang.org/x/text v0.30.0 // indirect
)

replace (
	github.com/agentflare-ai/agentml-go/env => ./env
	github.com/agentflare-ai/agentml-go/gemini => ./gemini
	github.com/agentflare-ai/agentml-go/memory => ./memory
	github.com/agentflare-ai/agentml-go/ollama => ./ollama
	github.com/agentflare-ai/agentml-go/prompt => ./prompt
	github.com/agentflare-ai/agentml-go/stdin => ./stdin
)
