package validator

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/agentflare-ai/go-xmldom"
	"github.com/agentflare-ai/go-xsd"
)

// GitHubSchemaLoader creates a schema loader function that fetches schemas from GitHub
// For a namespace like "github.com/foo/bar", it will try to fetch:
// - https://raw.githubusercontent.com/foo/bar/main/bar.xsd
// - https://raw.githubusercontent.com/foo/bar/master/bar.xsd
func GitHubSchemaLoader(httpClient *http.Client) xsd.SchemaLoaderFunc {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return func(attr xmldom.Attr) (*xsd.Schema, error) {
		// Extract namespace from attribute
		namespace := string(attr.NodeValue())
		slog.Debug("[GITHUB_LOADER] Loading schema for namespace", "namespace", namespace)

		// Parse GitHub namespace (e.g., "github.com/user/repo" or "github.com/user/repo/subpackage")
		if !strings.HasPrefix(namespace, "github.com/") {
			slog.Debug("[GITHUB_LOADER] Not a GitHub namespace", "namespace", namespace)
			return nil, fmt.Errorf("not a GitHub namespace: %s", namespace)
		}

		// Remove github.com/ prefix
		path := strings.TrimPrefix(namespace, "github.com/")
		parts := strings.Split(path, "/")

		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid GitHub namespace format: %s (expected github.com/user/repo)", namespace)
		}

		user := parts[0]
		repo := parts[1]

		// Schema filename depends on the namespace structure:
		// - github.com/user/repo -> repo.xsd
		// - github.com/user/repo/subpackage -> subpackage.xsd
		var schemaFile string
		if len(parts) > 2 {
			// Use the last path segment as the schema name
			schemaFile = parts[len(parts)-1] + ".xsd"
		} else {
			// Use the repo name as the schema name
			schemaFile = repo + ".xsd"
		}

		// Build URL attempts based on namespace structure
		// Strategy: Try repo-level schema first (most common), then subpackage schemas
		var attempts []string

		// Always try the repo-level XSD first (e.g., agentml.xsd for github.com/user/agentml/*)
		repoSchemaFile := repo + ".xsd"

		if len(parts) > 2 {
			// For namespaces with paths (e.g., github.com/user/repo/subpackage)
			// The path might be a namespace identifier, not a file path
			subpath := strings.Join(parts[2:], "/") // e.g., "subpackage" or "foo/bar"

			attempts = []string{
				// First priority: repo-level XSD (most common pattern)
				// e.g., agentml.xsd contains targetNamespace="github.com/agentflare-ai/agentml/agent"
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s/%s", user, repo, repo, repoSchemaFile),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s", user, repo, repoSchemaFile),

				// Second priority: subpackage-specific XSD
				// e.g., stdin.xsd for github.com/user/repo/stdin
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s/%s", user, repo, subpath, schemaFile),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s/%s", user, repo, repo, schemaFile),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s/%s/%s", user, repo, repo, subpath, schemaFile),

				// Try master branch variants
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/%s/%s", user, repo, repo, repoSchemaFile),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/%s", user, repo, repoSchemaFile),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/%s/%s", user, repo, subpath, schemaFile),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/%s/%s", user, repo, repo, schemaFile),
			}
		} else {
			// For repo-level namespaces (e.g., github.com/user/repo)
			attempts = []string{
				// Try repo/repo.xsd on main branch
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s/%s", user, repo, repo, repoSchemaFile),
				// Try repo.xsd at root on main branch
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/%s", user, repo, repoSchemaFile),
				// Try repo/repo.xsd on master branch
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/%s/%s", user, repo, repo, repoSchemaFile),
				// Try repo.xsd at root on master branch
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/%s", user, repo, repoSchemaFile),
			}
		}

		var lastErr error
		slog.Debug("[GITHUB_LOADER] Trying URLs", "attempts", attempts)
		for i, url := range attempts {
			slog.Debug("[GITHUB_LOADER] Attempting URL", "index", i, "url", url)
			schema, err := fetchAndParseSchema(httpClient, url)
			if err == nil && schema != nil {
				slog.Debug("[GITHUB_LOADER] Successfully loaded schema", "url", url)
				return schema, nil
			}
			slog.Debug("[GITHUB_LOADER] Failed to load from URL", "url", url, "error", err)
			lastErr = err
		}

		return nil, fmt.Errorf("failed to load schema from GitHub for %s: %w", namespace, lastErr)
	}
}

// fetchAndParseSchema fetches an XSD schema from a URL and parses it
func fetchAndParseSchema(client *http.Client, url string) (*xsd.Schema, error) {
	slog.Debug("[GITHUB_LOADER] Making HTTP request", "url", url)
	resp, err := client.Get(url)
	if err != nil {
		slog.Debug("[GITHUB_LOADER] HTTP request failed", "url", url, "error", err)
		return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	slog.Debug("[GITHUB_LOADER] HTTP response received", "url", url, "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		slog.Debug("[GITHUB_LOADER] HTTP error status", "url", url, "status", resp.StatusCode)
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	// Read the response body
	slog.Debug("[GITHUB_LOADER] Reading response body", "url", url)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug("[GITHUB_LOADER] Failed to read response body", "url", url, "error", err)
		return nil, fmt.Errorf("failed to read response from %s: %w", url, err)
	}

	slog.Debug("[GITHUB_LOADER] Response body read", "url", url, "bytes", len(body))

	// Parse the XML document
	slog.Debug("[GITHUB_LOADER] Parsing XML", "url", url)
	doc, err := xmldom.Decode(strings.NewReader(string(body)))
	if err != nil {
		slog.Debug("[GITHUB_LOADER] XML parsing failed", "url", url, "error", err)
		return nil, fmt.Errorf("failed to parse XML from %s: %w", url, err)
	}

	// Parse as XSD schema
	slog.Debug("[GITHUB_LOADER] Parsing XSD schema", "url", url)
	schema, err := xsd.Parse(doc)
	if err != nil {
		slog.Debug("[GITHUB_LOADER] XSD parsing failed", "url", url, "error", err)
		return nil, fmt.Errorf("failed to parse XSD schema from %s: %w", url, err)
	}

	slog.Debug("[GITHUB_LOADER] Schema parsed successfully", "url", url)
	return schema, nil
}
