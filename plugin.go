// Package main implements the LaunchNotes plugin for Relicta.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/relicta-tech/relicta-plugin-sdk/helpers"
	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// LaunchNotes GraphQL API endpoint.
var launchNotesGraphQLEndpoint = "https://app.launchnotes.io/graphql"

// httpClient is the HTTP client used for requests.
// Can be overridden in tests.
var httpClient HTTPClient = nil

// HTTPClient interface for HTTP operations (allows mocking in tests).
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// getHTTPClient returns the HTTP client to use for requests.
func getHTTPClient() HTTPClient {
	if httpClient != nil {
		return httpClient
	}
	return defaultHTTPClient
}

// Shared HTTP client for connection reuse across requests.
// Includes security hardening: TLS 1.3+, redirect protection.
var defaultHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to non-HTTPS URL not allowed")
		}
		// Only allow redirects within launchnotes.io domain
		if !strings.HasSuffix(req.URL.Host, "launchnotes.io") {
			return fmt.Errorf("redirect away from launchnotes.io not allowed")
		}
		return nil
	},
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	},
}

// LaunchNotesPlugin implements the LaunchNotes announcement plugin.
type LaunchNotesPlugin struct{}

// Config represents the LaunchNotes plugin configuration.
type Config struct {
	// APIToken is the LaunchNotes API token (Management token required for creating announcements).
	APIToken string `json:"api_token,omitempty"`
	// ProjectID is the LaunchNotes project ID.
	ProjectID string `json:"project_id,omitempty"`
	// CreateDraft creates the announcement as a draft instead of publishing.
	CreateDraft bool `json:"create_draft"`
	// AutoPublish automatically publishes the announcement after creation.
	AutoPublish bool `json:"auto_publish"`
	// Categories is a list of category IDs to associate with the announcement.
	Categories []string `json:"categories,omitempty"`
	// ChangeTypes is a list of change type IDs (e.g., "new", "improved", "fixed").
	ChangeTypes []string `json:"change_types,omitempty"`
	// IncludeChangelog includes the full changelog in the announcement content.
	IncludeChangelog bool `json:"include_changelog"`
	// TitleTemplate is a template for the announcement title.
	TitleTemplate string `json:"title_template,omitempty"`
	// NotifySubscribers sends email notifications to subscribers.
	NotifySubscribers bool `json:"notify_subscribers"`
}

// GraphQLRequest represents a GraphQL request payload.
type GraphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// GraphQLResponse represents a GraphQL response.
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data,omitempty"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error.
type GraphQLError struct {
	Message    string   `json:"message"`
	Path       []string `json:"path,omitempty"`
	Extensions any      `json:"extensions,omitempty"`
}

// CreateAnnouncementResponse represents the response from createAnnouncement mutation.
type CreateAnnouncementResponse struct {
	CreateAnnouncement struct {
		Announcement struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			State       string `json:"state"`
			PublishedAt string `json:"publishedAt,omitempty"`
		} `json:"announcement"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors,omitempty"`
	} `json:"createAnnouncement"`
}

// PublishAnnouncementResponse represents the response from publishAnnouncement mutation.
type PublishAnnouncementResponse struct {
	PublishAnnouncement struct {
		Announcement struct {
			ID          string `json:"id"`
			State       string `json:"state"`
			PublishedAt string `json:"publishedAt"`
		} `json:"announcement"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors,omitempty"`
	} `json:"publishAnnouncement"`
}

// GetInfo returns plugin metadata.
func (p *LaunchNotesPlugin) GetInfo() plugin.Info {
	return plugin.Info{
		Name:        "launchnotes",
		Version:     "2.0.0",
		Description: "Create and publish announcements to LaunchNotes",
		Author:      "Relicta Team",
		Hooks: []plugin.Hook{
			plugin.HookPostPublish,
			plugin.HookOnSuccess,
		},
		ConfigSchema: `{
			"type": "object",
			"properties": {
				"api_token": {"type": "string", "description": "LaunchNotes API token (or use LAUNCHNOTES_API_TOKEN env)"},
				"project_id": {"type": "string", "description": "LaunchNotes project ID (or use LAUNCHNOTES_PROJECT_ID env)"},
				"create_draft": {"type": "boolean", "description": "Create as draft instead of publishing", "default": false},
				"auto_publish": {"type": "boolean", "description": "Auto-publish after creation", "default": true},
				"categories": {"type": "array", "items": {"type": "string"}, "description": "Category IDs to associate"},
				"change_types": {"type": "array", "items": {"type": "string"}, "description": "Change type IDs (new, improved, fixed)"},
				"include_changelog": {"type": "boolean", "description": "Include full changelog in content", "default": true},
				"title_template": {"type": "string", "description": "Title template", "default": "Release {{version}}"},
				"notify_subscribers": {"type": "boolean", "description": "Send email notifications", "default": false}
			},
			"required": ["api_token", "project_id"]
		}`,
	}
}

// Execute runs the plugin for a given hook.
func (p *LaunchNotesPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish, plugin.HookOnSuccess:
		return p.createAnnouncement(ctx, cfg, req.Context, req.DryRun)
	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: fmt.Sprintf("Hook %s not handled", req.Hook),
		}, nil
	}
}

// createAnnouncement creates a new announcement in LaunchNotes.
func (p *LaunchNotesPlugin) createAnnouncement(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	// Build title
	title := cfg.TitleTemplate
	if title == "" {
		title = "Release {{version}}"
	}
	title = strings.ReplaceAll(title, "{{version}}", releaseCtx.Version)

	// Build content
	var content strings.Builder
	content.WriteString(fmt.Sprintf("# %s\n\n", title))

	if releaseCtx.ReleaseNotes != "" {
		// Escape HTML to prevent XSS attacks in release notes
		content.WriteString(html.EscapeString(releaseCtx.ReleaseNotes))
	} else if cfg.IncludeChangelog && releaseCtx.Changelog != "" {
		// Escape HTML to prevent XSS attacks in changelog
		content.WriteString(html.EscapeString(releaseCtx.Changelog))
	} else if releaseCtx.Changes != nil {
		// Build content from changes
		if len(releaseCtx.Changes.Breaking) > 0 {
			content.WriteString("\n## Breaking Changes\n")
			for _, c := range releaseCtx.Changes.Breaking {
				content.WriteString(fmt.Sprintf("- %s\n", c.Description))
			}
		}
		if len(releaseCtx.Changes.Features) > 0 {
			content.WriteString("\n## New Features\n")
			for _, c := range releaseCtx.Changes.Features {
				content.WriteString(fmt.Sprintf("- %s\n", c.Description))
			}
		}
		if len(releaseCtx.Changes.Fixes) > 0 {
			content.WriteString("\n## Bug Fixes\n")
			for _, c := range releaseCtx.Changes.Fixes {
				content.WriteString(fmt.Sprintf("- %s\n", c.Description))
			}
		}
	}

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Would create LaunchNotes announcement",
			Outputs: map[string]any{
				"title":      title,
				"project_id": cfg.ProjectID,
				"draft":      cfg.CreateDraft,
			},
		}, nil
	}

	// Create announcement via GraphQL
	announcementID, err := p.executeCreateAnnouncement(ctx, cfg, title, content.String())
	if err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create announcement: %v", err),
		}, nil
	}

	// Publish if auto_publish is enabled and not creating as draft
	if cfg.AutoPublish && !cfg.CreateDraft {
		if err := p.executePublishAnnouncement(ctx, cfg, announcementID); err != nil {
			return &plugin.ExecuteResponse{
				Success: false,
				Error:   fmt.Sprintf("created announcement but failed to publish: %v", err),
				Outputs: map[string]any{
					"announcement_id": announcementID,
				},
			}, nil
		}
	}

	state := "published"
	if cfg.CreateDraft || !cfg.AutoPublish {
		state = "draft"
	}

	return &plugin.ExecuteResponse{
		Success: true,
		Message: fmt.Sprintf("Created LaunchNotes announcement (%s)", state),
		Outputs: map[string]any{
			"announcement_id": announcementID,
			"title":           title,
			"state":           state,
		},
	}, nil
}

// executeCreateAnnouncement executes the GraphQL mutation to create an announcement.
func (p *LaunchNotesPlugin) executeCreateAnnouncement(ctx context.Context, cfg *Config, title, content string) (string, error) {
	mutation := `
		mutation CreateAnnouncement($input: CreateAnnouncementInput!) {
			createAnnouncement(input: $input) {
				announcement {
					id
					title
					state
					publishedAt
				}
				errors {
					message
				}
			}
		}
	`

	variables := map[string]any{
		"input": map[string]any{
			"projectId": cfg.ProjectID,
			"title":     title,
			"content":   content,
		},
	}

	// Add optional fields
	if len(cfg.Categories) > 0 {
		variables["input"].(map[string]any)["categoryIds"] = cfg.Categories
	}
	if len(cfg.ChangeTypes) > 0 {
		variables["input"].(map[string]any)["changeTypeIds"] = cfg.ChangeTypes
	}

	resp, err := p.executeGraphQL(ctx, cfg.APIToken, mutation, variables)
	if err != nil {
		return "", err
	}

	var result CreateAnnouncementResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.CreateAnnouncement.Errors) > 0 {
		return "", fmt.Errorf("LaunchNotes error: %s", result.CreateAnnouncement.Errors[0].Message)
	}

	return result.CreateAnnouncement.Announcement.ID, nil
}

// executePublishAnnouncement executes the GraphQL mutation to publish an announcement.
func (p *LaunchNotesPlugin) executePublishAnnouncement(ctx context.Context, cfg *Config, announcementID string) error {
	mutation := `
		mutation PublishAnnouncement($input: PublishAnnouncementInput!) {
			publishAnnouncement(input: $input) {
				announcement {
					id
					state
					publishedAt
				}
				errors {
					message
				}
			}
		}
	`

	variables := map[string]any{
		"input": map[string]any{
			"announcementId":    announcementID,
			"notifySubscribers": cfg.NotifySubscribers,
		},
	}

	resp, err := p.executeGraphQL(ctx, cfg.APIToken, mutation, variables)
	if err != nil {
		return err
	}

	var result PublishAnnouncementResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.PublishAnnouncement.Errors) > 0 {
		return fmt.Errorf("LaunchNotes error: %s", result.PublishAnnouncement.Errors[0].Message)
	}

	return nil
}

// executeGraphQL sends a GraphQL request to LaunchNotes.
func (p *LaunchNotesPlugin) executeGraphQL(ctx context.Context, token, query string, variables map[string]any) (*GraphQLResponse, error) {
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", launchNotesGraphQLEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := getHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LaunchNotes returned status %d: %s", resp.StatusCode, string(body))
	}

	var graphQLResp GraphQLResponse
	if err := json.Unmarshal(body, &graphQLResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", graphQLResp.Errors[0].Message)
	}

	return &graphQLResp, nil
}

// parseConfig parses the plugin configuration using the shared ConfigParser.
func (p *LaunchNotesPlugin) parseConfig(raw map[string]any) *Config {
	parser := helpers.NewConfigParser(raw)

	// Get API token from config or env
	apiToken := parser.GetString("api_token", "LAUNCHNOTES_API_TOKEN", "")

	// Get project ID from config or env
	projectID := parser.GetString("project_id", "LAUNCHNOTES_PROJECT_ID", "")

	return &Config{
		APIToken:          apiToken,
		ProjectID:         projectID,
		CreateDraft:       parser.GetBool("create_draft", false),
		AutoPublish:       parser.GetBool("auto_publish", true),
		Categories:        parser.GetStringSlice("categories", nil),
		ChangeTypes:       parser.GetStringSlice("change_types", nil),
		IncludeChangelog:  parser.GetBool("include_changelog", true),
		TitleTemplate:     parser.GetString("title_template", "", "Release {{version}}"),
		NotifySubscribers: parser.GetBool("notify_subscribers", false),
	}
}

// Validate validates the plugin configuration.
func (p *LaunchNotesPlugin) Validate(_ context.Context, config map[string]any) (*plugin.ValidateResponse, error) {
	vb := helpers.NewValidationBuilder()
	parser := helpers.NewConfigParser(config)

	// Validate API token
	token := parser.GetString("api_token", "LAUNCHNOTES_API_TOKEN", "")
	if token == "" {
		// Also check env directly
		if os.Getenv("LAUNCHNOTES_API_TOKEN") == "" {
			vb.AddError("api_token",
				"LaunchNotes API token is required (set LAUNCHNOTES_API_TOKEN env var or configure api_token)")
		}
	}

	// Validate project ID
	projectID := parser.GetString("project_id", "LAUNCHNOTES_PROJECT_ID", "")
	if projectID == "" {
		// Also check env directly
		if os.Getenv("LAUNCHNOTES_PROJECT_ID") == "" {
			vb.AddError("project_id",
				"LaunchNotes project ID is required (set LAUNCHNOTES_PROJECT_ID env var or configure project_id)")
		}
	}

	return vb.Build(), nil
}
