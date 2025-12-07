package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/agentflare-ai/agentmlx/validator"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: validate <scxml-file>")
		os.Exit(1)
	}

	xmlFile := os.Args[1]

	// Read XML file
	xmlData, err := os.ReadFile(xmlFile)
	if err != nil {
		log.Fatalf("Failed to read XML file: %v", err)
	}

	// Create validator
	v := validator.New(validator.Config{
		SourceName: xmlFile,
	})

	// Validate
	ctx := context.Background()
	result, _, err := v.ValidateString(ctx, string(xmlData))
	if err != nil {
		log.Fatalf("Validation error: %v", err)
	}

	// Print results
	if len(result.Diagnostics) == 0 {
		fmt.Printf("✅ %s is valid!\n", xmlFile)
		os.Exit(0)
	}

	// Print diagnostics
	reporter := validator.NewPrettyReporter(os.Stdout, validator.PrettyConfig{
		Color:           true,
		ShowFullElement: false,
		ContextBefore:   1,
		ContextAfter:    1,
	})

	if err := reporter.Print(xmlFile, string(xmlData), result.Diagnostics); err != nil {
		log.Fatalf("Failed to print diagnostics: %v", err)
	}

	if result.HasErrors() {
		os.Exit(1)
	}
}
