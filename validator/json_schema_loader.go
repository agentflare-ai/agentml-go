package validator

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentflare-ai/go-jsonschema"
)

// LoadSchemaFromURI loads a JSON schema from a URI
// Supports file:// (relative to baseDir) and github.com/ URLs
func LoadSchemaFromURI(uri string, baseDir string) (*jsonschema.Schema, error) {
	// Determine schema source type
	if strings.HasPrefix(uri, "file://") {
		return LoadFileSchema(uri, baseDir)
	} else if strings.HasPrefix(uri, "github.com/") {
		return LoadGitHubSchema(uri)
	}

	return nil, fmt.Errorf("unsupported schema URI format: %s (expected file:// or github.com/)", uri)
}

// LoadFileSchema loads a JSON schema from a file:// URL
// Paths are resolved relative to baseDir
func LoadFileSchema(fileURI string, baseDir string) (*jsonschema.Schema, error) {
	// Remove file:// prefix
	path := strings.TrimPrefix(fileURI, "file://")

	// Resolve relative to base directory
	var absPath string
	if filepath.IsAbs(path) {
		absPath = path
	} else {
		if baseDir == "" {
			baseDir = "."
		}
		absPath = filepath.Join(baseDir, path)
	}

	slog.Debug("[FILE_SCHEMA_LOADER] Loading schema from file", "uri", fileURI, "absPath", absPath)

	// Read the file
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file %s: %w", absPath, err)
	}

	// Parse JSON schema
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse JSON schema from %s: %w", absPath, err)
	}

	slog.Debug("[FILE_SCHEMA_LOADER] Successfully loaded schema", "uri", fileURI)
	return &schema, nil
}

// LoadGitHubSchema loads a JSON schema from a GitHub URL
// Converts github.com/user/repo/path/to/schema.json to raw.githubusercontent.com URL
func LoadGitHubSchema(githubURL string) (*jsonschema.Schema, error) {
	// Parse GitHub URL: github.com/user/repo/path/to/file.json
	path := strings.TrimPrefix(githubURL, "github.com/")
	parts := strings.SplitN(path, "/", 3)

	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid GitHub URL format: %s (expected github.com/user/repo/path/to/schema.json)", githubURL)
	}

	user := parts[0]
	repo := parts[1]
	filePath := parts[2]

	// Try main branch first, then master
	attempts := []string{
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s", user, repo, filePath),
		fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/%s", user, repo, filePath),
	}

	var lastErr error
	client := &http.Client{Timeout: 30 * time.Second}

	for i, url := range attempts {
		slog.Debug("[GITHUB_SCHEMA_LOADER] Attempting URL", "index", i, "url", url)

		schema, err := fetchAndParseJSONSchema(client, url)
		if err == nil {
			slog.Debug("[GITHUB_SCHEMA_LOADER] Successfully loaded schema", "url", url)
			return schema, nil
		}

		slog.Debug("[GITHUB_SCHEMA_LOADER] Failed to load from URL", "url", url, "error", err)
		lastErr = err
	}

	return nil, fmt.Errorf("failed to load schema from GitHub for %s: %w", githubURL, lastErr)
}

// fetchAndParseJSONSchema fetches a JSON schema from a URL and parses it
func fetchAndParseJSONSchema(client *http.Client, url string) (*jsonschema.Schema, error) {
	slog.Debug("[GITHUB_SCHEMA_LOADER] Making HTTP request", "url", url)

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	slog.Debug("[GITHUB_SCHEMA_LOADER] HTTP response received", "url", url, "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from %s: %w", url, err)
	}

	slog.Debug("[GITHUB_SCHEMA_LOADER] Response body read", "url", url, "bytes", len(body))

	// Parse JSON schema
	var schema jsonschema.Schema
	if err := json.Unmarshal(body, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse JSON schema from %s: %w", url, err)
	}

	slog.Debug("[GITHUB_SCHEMA_LOADER] Schema parsed successfully", "url", url)
	return &schema, nil
}
