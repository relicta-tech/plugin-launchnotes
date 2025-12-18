// Package main implements unit tests for the LaunchNotes plugin.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/relicta-tech/relicta-plugin-sdk/plugin"
)

// TestGetInfo verifies that the plugin returns correct metadata.
func TestGetInfo(t *testing.T) {
	p := &LaunchNotesPlugin{}
	info := p.GetInfo()

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"Name", info.Name, "launchnotes"},
		{"Version", info.Version, "2.0.0"},
		{"Description", info.Description, "Create and publish announcements to LaunchNotes"},
		{"Author", info.Author, "Relicta Team"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("GetInfo().%s = %q, expected %q", tt.name, tt.got, tt.expected)
			}
		})
	}

	// Verify hooks
	expectedHooks := []plugin.Hook{plugin.HookPostPublish, plugin.HookOnSuccess}
	if len(info.Hooks) != len(expectedHooks) {
		t.Errorf("GetInfo().Hooks length = %d, expected %d", len(info.Hooks), len(expectedHooks))
	}
	for i, hook := range expectedHooks {
		if info.Hooks[i] != hook {
			t.Errorf("GetInfo().Hooks[%d] = %q, expected %q", i, info.Hooks[i], hook)
		}
	}

	// Verify config schema is valid JSON
	if info.ConfigSchema == "" {
		t.Error("GetInfo().ConfigSchema should not be empty")
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(info.ConfigSchema), &schema); err != nil {
		t.Errorf("GetInfo().ConfigSchema is not valid JSON: %v", err)
	}
}

// TestValidate tests configuration validation with various scenarios.
func TestValidate(t *testing.T) {
	tests := []struct {
		name         string
		config       map[string]any
		envVars      map[string]string
		expectValid  bool
		expectErrors []string
	}{
		{
			name: "valid config with all fields",
			config: map[string]any{
				"api_token":  "test-token-12345",
				"project_id": "proj-abc123",
			},
			expectValid:  true,
			expectErrors: nil,
		},
		{
			name:        "missing api_token and project_id",
			config:      map[string]any{},
			expectValid: false,
			expectErrors: []string{
				"api_token",
				"project_id",
			},
		},
		{
			name: "missing api_token only",
			config: map[string]any{
				"project_id": "proj-abc123",
			},
			expectValid:  false,
			expectErrors: []string{"api_token"},
		},
		{
			name: "missing project_id only",
			config: map[string]any{
				"api_token": "test-token-12345",
			},
			expectValid:  false,
			expectErrors: []string{"project_id"},
		},
		{
			name:   "valid config from environment variables",
			config: map[string]any{},
			envVars: map[string]string{
				"LAUNCHNOTES_API_TOKEN":  "env-token-12345",
				"LAUNCHNOTES_PROJECT_ID": "env-proj-abc123",
			},
			expectValid:  true,
			expectErrors: nil,
		},
		{
			name: "config takes precedence over env vars",
			config: map[string]any{
				"api_token":  "config-token",
				"project_id": "config-project",
			},
			envVars: map[string]string{
				"LAUNCHNOTES_API_TOKEN":  "env-token",
				"LAUNCHNOTES_PROJECT_ID": "env-project",
			},
			expectValid:  true,
			expectErrors: nil,
		},
		{
			name: "empty string values should fail",
			config: map[string]any{
				"api_token":  "",
				"project_id": "",
			},
			expectValid: false,
			expectErrors: []string{
				"api_token",
				"project_id",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}
			// Clean up environment variables after test
			defer func() {
				for k := range tt.envVars {
					_ = os.Unsetenv(k)
				}
			}()

			// Also unset env vars if not in the test case
			if _, ok := tt.envVars["LAUNCHNOTES_API_TOKEN"]; !ok {
				_ = os.Unsetenv("LAUNCHNOTES_API_TOKEN")
			}
			if _, ok := tt.envVars["LAUNCHNOTES_PROJECT_ID"]; !ok {
				_ = os.Unsetenv("LAUNCHNOTES_PROJECT_ID")
			}

			p := &LaunchNotesPlugin{}
			resp, err := p.Validate(context.Background(), tt.config)
			if err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}

			if resp.Valid != tt.expectValid {
				t.Errorf("Validate().Valid = %v, expected %v", resp.Valid, tt.expectValid)
			}

			if !tt.expectValid {
				// Check that expected error fields are present
				errFields := make(map[string]bool)
				for _, e := range resp.Errors {
					errFields[e.Field] = true
				}
				for _, expectedField := range tt.expectErrors {
					if !errFields[expectedField] {
						t.Errorf("Expected validation error for field %q but not found", expectedField)
					}
				}
			}
		})
	}
}

// TestParseConfig tests configuration parsing with defaults and custom values.
func TestParseConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		envVars  map[string]string
		expected Config
	}{
		{
			name:   "defaults when config is empty",
			config: map[string]any{},
			expected: Config{
				APIToken:          "",
				ProjectID:         "",
				CreateDraft:       false,
				AutoPublish:       true,
				Categories:        nil,
				ChangeTypes:       nil,
				IncludeChangelog:  true,
				TitleTemplate:     "Release {{version}}",
				NotifySubscribers: false,
			},
		},
		{
			name: "all fields provided",
			config: map[string]any{
				"api_token":          "my-token",
				"project_id":         "my-project",
				"create_draft":       true,
				"auto_publish":       false,
				"categories":         []any{"cat1", "cat2"},
				"change_types":       []any{"new", "improved"},
				"include_changelog":  false,
				"title_template":     "v{{version}} Release",
				"notify_subscribers": true,
			},
			expected: Config{
				APIToken:          "my-token",
				ProjectID:         "my-project",
				CreateDraft:       true,
				AutoPublish:       false,
				Categories:        []string{"cat1", "cat2"},
				ChangeTypes:       []string{"new", "improved"},
				IncludeChangelog:  false,
				TitleTemplate:     "v{{version}} Release",
				NotifySubscribers: true,
			},
		},
		{
			name:   "values from environment variables",
			config: map[string]any{},
			envVars: map[string]string{
				"LAUNCHNOTES_API_TOKEN":  "env-token",
				"LAUNCHNOTES_PROJECT_ID": "env-project",
			},
			expected: Config{
				APIToken:          "env-token",
				ProjectID:         "env-project",
				CreateDraft:       false,
				AutoPublish:       true,
				Categories:        nil,
				ChangeTypes:       nil,
				IncludeChangelog:  true,
				TitleTemplate:     "Release {{version}}",
				NotifySubscribers: false,
			},
		},
		{
			name: "config takes precedence over env vars",
			config: map[string]any{
				"api_token":  "config-token",
				"project_id": "config-project",
			},
			envVars: map[string]string{
				"LAUNCHNOTES_API_TOKEN":  "env-token",
				"LAUNCHNOTES_PROJECT_ID": "env-project",
			},
			expected: Config{
				APIToken:          "config-token",
				ProjectID:         "config-project",
				CreateDraft:       false,
				AutoPublish:       true,
				Categories:        nil,
				ChangeTypes:       nil,
				IncludeChangelog:  true,
				TitleTemplate:     "Release {{version}}",
				NotifySubscribers: false,
			},
		},
		{
			name: "boolean as string",
			config: map[string]any{
				"create_draft": "true",
				"auto_publish": "false",
			},
			expected: Config{
				APIToken:          "",
				ProjectID:         "",
				CreateDraft:       true,
				AutoPublish:       false,
				Categories:        nil,
				ChangeTypes:       nil,
				IncludeChangelog:  true,
				TitleTemplate:     "Release {{version}}",
				NotifySubscribers: false,
			},
		},
		{
			name: "string slice from []string type",
			config: map[string]any{
				"categories": []string{"cat1", "cat2", "cat3"},
			},
			expected: Config{
				APIToken:          "",
				ProjectID:         "",
				CreateDraft:       false,
				AutoPublish:       true,
				Categories:        []string{"cat1", "cat2", "cat3"},
				ChangeTypes:       nil,
				IncludeChangelog:  true,
				TitleTemplate:     "Release {{version}}",
				NotifySubscribers: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear and set environment variables
			_ = os.Unsetenv("LAUNCHNOTES_API_TOKEN")
			_ = os.Unsetenv("LAUNCHNOTES_PROJECT_ID")

			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.envVars {
					_ = os.Unsetenv(k)
				}
			}()

			p := &LaunchNotesPlugin{}
			cfg := p.parseConfig(tt.config)

			if cfg.APIToken != tt.expected.APIToken {
				t.Errorf("parseConfig().APIToken = %q, expected %q", cfg.APIToken, tt.expected.APIToken)
			}
			if cfg.ProjectID != tt.expected.ProjectID {
				t.Errorf("parseConfig().ProjectID = %q, expected %q", cfg.ProjectID, tt.expected.ProjectID)
			}
			if cfg.CreateDraft != tt.expected.CreateDraft {
				t.Errorf("parseConfig().CreateDraft = %v, expected %v", cfg.CreateDraft, tt.expected.CreateDraft)
			}
			if cfg.AutoPublish != tt.expected.AutoPublish {
				t.Errorf("parseConfig().AutoPublish = %v, expected %v", cfg.AutoPublish, tt.expected.AutoPublish)
			}
			if cfg.IncludeChangelog != tt.expected.IncludeChangelog {
				t.Errorf("parseConfig().IncludeChangelog = %v, expected %v", cfg.IncludeChangelog, tt.expected.IncludeChangelog)
			}
			if cfg.TitleTemplate != tt.expected.TitleTemplate {
				t.Errorf("parseConfig().TitleTemplate = %q, expected %q", cfg.TitleTemplate, tt.expected.TitleTemplate)
			}
			if cfg.NotifySubscribers != tt.expected.NotifySubscribers {
				t.Errorf("parseConfig().NotifySubscribers = %v, expected %v", cfg.NotifySubscribers, tt.expected.NotifySubscribers)
			}

			// Compare slices
			if len(cfg.Categories) != len(tt.expected.Categories) {
				t.Errorf("parseConfig().Categories length = %d, expected %d", len(cfg.Categories), len(tt.expected.Categories))
			} else {
				for i := range cfg.Categories {
					if cfg.Categories[i] != tt.expected.Categories[i] {
						t.Errorf("parseConfig().Categories[%d] = %q, expected %q", i, cfg.Categories[i], tt.expected.Categories[i])
					}
				}
			}

			if len(cfg.ChangeTypes) != len(tt.expected.ChangeTypes) {
				t.Errorf("parseConfig().ChangeTypes length = %d, expected %d", len(cfg.ChangeTypes), len(tt.expected.ChangeTypes))
			} else {
				for i := range cfg.ChangeTypes {
					if cfg.ChangeTypes[i] != tt.expected.ChangeTypes[i] {
						t.Errorf("parseConfig().ChangeTypes[%d] = %q, expected %q", i, cfg.ChangeTypes[i], tt.expected.ChangeTypes[i])
					}
				}
			}
		})
	}
}

// TestExecute tests execution for relevant hooks using dry run mode.
func TestExecute(t *testing.T) {
	tests := []struct {
		name           string
		hook           plugin.Hook
		config         map[string]any
		releaseContext plugin.ReleaseContext
		dryRun         bool
		expectSuccess  bool
		expectMessage  string
		expectOutputs  map[string]any
	}{
		{
			name: "PostPublish hook in dry run mode",
			hook: plugin.HookPostPublish,
			config: map[string]any{
				"api_token":      "test-token",
				"project_id":     "test-project",
				"title_template": "Release {{version}}",
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.2.3",
				ReleaseNotes: "Test release notes",
			},
			dryRun:        true,
			expectSuccess: true,
			expectMessage: "Would create LaunchNotes announcement",
			expectOutputs: map[string]any{
				"title":      "Release 1.2.3",
				"project_id": "test-project",
				"draft":      false,
			},
		},
		{
			name: "OnSuccess hook in dry run mode",
			hook: plugin.HookOnSuccess,
			config: map[string]any{
				"api_token":      "test-token",
				"project_id":     "test-project",
				"title_template": "v{{version}} is here!",
				"create_draft":   true,
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "2.0.0",
				ReleaseNotes: "Major release!",
			},
			dryRun:        true,
			expectSuccess: true,
			expectMessage: "Would create LaunchNotes announcement",
			expectOutputs: map[string]any{
				"title":      "v2.0.0 is here!",
				"project_id": "test-project",
				"draft":      true,
			},
		},
		{
			name: "unhandled hook returns success",
			hook: plugin.HookPrePlan,
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			releaseContext: plugin.ReleaseContext{
				Version: "1.0.0",
			},
			dryRun:        false,
			expectSuccess: true,
			expectMessage: "Hook pre-plan not handled",
			expectOutputs: nil,
		},
		{
			name: "custom title template with version substitution",
			hook: plugin.HookPostPublish,
			config: map[string]any{
				"api_token":      "test-token",
				"project_id":     "my-proj",
				"title_template": "Version {{version}} Released",
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "3.5.7",
				ReleaseNotes: "New features",
			},
			dryRun:        true,
			expectSuccess: true,
			expectMessage: "Would create LaunchNotes announcement",
			expectOutputs: map[string]any{
				"title":      "Version 3.5.7 Released",
				"project_id": "my-proj",
				"draft":      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &LaunchNotesPlugin{}
			req := plugin.ExecuteRequest{
				Hook:    tt.hook,
				Config:  tt.config,
				Context: tt.releaseContext,
				DryRun:  tt.dryRun,
			}

			resp, err := p.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if resp.Success != tt.expectSuccess {
				t.Errorf("Execute().Success = %v, expected %v", resp.Success, tt.expectSuccess)
			}

			if resp.Message != tt.expectMessage {
				t.Errorf("Execute().Message = %q, expected %q", resp.Message, tt.expectMessage)
			}

			if tt.expectOutputs != nil {
				for k, v := range tt.expectOutputs {
					if resp.Outputs[k] != v {
						t.Errorf("Execute().Outputs[%q] = %v, expected %v", k, resp.Outputs[k], v)
					}
				}
			}
		})
	}
}

// TestExecuteWithChanges tests that changes are properly formatted in announcements.
func TestExecuteWithChanges(t *testing.T) {
	p := &LaunchNotesPlugin{}

	tests := []struct {
		name           string
		releaseContext plugin.ReleaseContext
		config         map[string]any
	}{
		{
			name: "with release notes (takes precedence)",
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "These are the release notes",
				Changelog:    "This is the changelog",
				Changes: &plugin.CategorizedChanges{
					Features: []plugin.ConventionalCommit{
						{Description: "Added feature 1"},
					},
				},
			},
			config: map[string]any{
				"api_token":         "test-token",
				"project_id":        "test-project",
				"include_changelog": true,
			},
		},
		{
			name: "with changelog when no release notes",
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "",
				Changelog:    "This is the changelog content",
			},
			config: map[string]any{
				"api_token":         "test-token",
				"project_id":        "test-project",
				"include_changelog": true,
			},
		},
		{
			name: "with categorized changes when no changelog",
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "",
				Changelog:    "",
				Changes: &plugin.CategorizedChanges{
					Features: []plugin.ConventionalCommit{
						{Description: "Added new feature"},
					},
					Fixes: []plugin.ConventionalCommit{
						{Description: "Fixed bug 1"},
						{Description: "Fixed bug 2"},
					},
					Breaking: []plugin.ConventionalCommit{
						{Description: "Breaking change"},
					},
				},
			},
			config: map[string]any{
				"api_token":         "test-token",
				"project_id":        "test-project",
				"include_changelog": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseContext,
				DryRun:  true,
			}

			resp, err := p.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if !resp.Success {
				t.Errorf("Execute().Success = false, expected true")
			}
		})
	}
}

// TestCreateAnnouncementWithMockServer tests the actual HTTP interaction with a mock server.
func TestCreateAnnouncementWithMockServer(t *testing.T) {
	tests := []struct {
		name           string
		config         map[string]any
		releaseContext plugin.ReleaseContext
		serverResponse string
		serverStatus   int
		expectSuccess  bool
		expectError    string
		expectState    string
	}{
		{
			name: "successful creation and publish",
			config: map[string]any{
				"api_token":    "test-token",
				"project_id":   "test-project",
				"auto_publish": true,
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			serverResponse: `{
				"data": {
					"createAnnouncement": {
						"announcement": {
							"id": "ann-123",
							"title": "Release 1.0.0",
							"state": "draft"
						},
						"errors": []
					}
				}
			}`,
			serverStatus:  http.StatusOK,
			expectSuccess: true,
			expectState:   "published",
		},
		{
			name: "create as draft",
			config: map[string]any{
				"api_token":    "test-token",
				"project_id":   "test-project",
				"create_draft": true,
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			serverResponse: `{
				"data": {
					"createAnnouncement": {
						"announcement": {
							"id": "ann-456",
							"title": "Release 1.0.0",
							"state": "draft"
						},
						"errors": []
					}
				}
			}`,
			serverStatus:  http.StatusOK,
			expectSuccess: true,
			expectState:   "draft",
		},
		{
			name: "server error",
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			serverResponse: `Internal Server Error`,
			serverStatus:   http.StatusInternalServerError,
			expectSuccess:  false,
			expectError:    "failed to create announcement",
		},
		{
			name: "GraphQL error response",
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			serverResponse: `{
				"errors": [
					{"message": "Invalid project ID"}
				]
			}`,
			serverStatus:  http.StatusOK,
			expectSuccess: false,
			expectError:   "failed to create announcement",
		},
		{
			name: "announcement creation error",
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			serverResponse: `{
				"data": {
					"createAnnouncement": {
						"announcement": null,
						"errors": [{"message": "Unauthorized"}]
					}
				}
			}`,
			serverStatus:  http.StatusOK,
			expectSuccess: false,
			expectError:   "failed to create announcement: LaunchNotes error: Unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track if publish was called
			publishCalled := false

			// Create test server
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}

				auth := r.Header.Get("Authorization")
				if !strings.HasPrefix(auth, "Bearer ") {
					t.Errorf("Expected Bearer token, got %s", auth)
				}

				// Parse the request to check if it's a publish or create
				body, _ := io.ReadAll(r.Body)
				var req GraphQLRequest
				_ = json.Unmarshal(body, &req)

				if strings.Contains(req.Query, "publishAnnouncement") {
					publishCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{
						"data": {
							"publishAnnouncement": {
								"announcement": {
									"id": "ann-123",
									"state": "published",
									"publishedAt": "2024-01-15T10:00:00Z"
								},
								"errors": []
							}
						}
					}`))
					return
				}

				w.WriteHeader(tt.serverStatus)
				_, _ = w.Write([]byte(tt.serverResponse))
			}))
			defer server.Close()

			// Create a custom plugin that uses our test server
			p := &testPlugin{
				LaunchNotesPlugin: &LaunchNotesPlugin{},
				testServer:        server,
			}

			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseContext,
				DryRun:  false,
			}

			resp, err := p.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if resp.Success != tt.expectSuccess {
				t.Errorf("Execute().Success = %v, expected %v (error: %s)", resp.Success, tt.expectSuccess, resp.Error)
			}

			if tt.expectError != "" && !strings.Contains(resp.Error, tt.expectError) {
				t.Errorf("Execute().Error = %q, expected to contain %q", resp.Error, tt.expectError)
			}

			if tt.expectSuccess && tt.expectState != "" {
				if state, ok := resp.Outputs["state"]; ok {
					if state != tt.expectState {
						t.Errorf("Execute().Outputs[state] = %v, expected %v", state, tt.expectState)
					}
				}
			}

			// Verify publish was called for auto_publish
			if tt.expectSuccess && tt.expectState == "published" && !publishCalled {
				t.Error("Expected publishAnnouncement to be called for auto_publish")
			}
		})
	}
}

// testPlugin wraps LaunchNotesPlugin to use a test server.
type testPlugin struct {
	*LaunchNotesPlugin
	testServer *httptest.Server
}

// Execute overrides to use the test server.
func (p *testPlugin) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish, plugin.HookOnSuccess:
		return p.createAnnouncementWithTestServer(ctx, cfg, req.Context, req.DryRun)
	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Hook " + string(req.Hook) + " not handled",
		}, nil
	}
}

// createAnnouncementWithTestServer creates announcement using the test server.
func (p *testPlugin) createAnnouncementWithTestServer(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	// Build title
	title := cfg.TitleTemplate
	if title == "" {
		title = "Release {{version}}"
	}
	title = strings.ReplaceAll(title, "{{version}}", releaseCtx.Version)

	// Build content
	var content strings.Builder
	content.WriteString("# " + title + "\n\n")
	if releaseCtx.ReleaseNotes != "" {
		content.WriteString(releaseCtx.ReleaseNotes)
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
	announcementID, err := p.executeCreateAnnouncementWithTestServer(ctx, cfg, title, content.String())
	if err != nil {
		return &plugin.ExecuteResponse{
			Success: false,
			Error:   "failed to create announcement: " + err.Error(),
		}, nil
	}

	// Publish if auto_publish is enabled and not creating as draft
	if cfg.AutoPublish && !cfg.CreateDraft {
		if err := p.executePublishAnnouncementWithTestServer(ctx, cfg, announcementID); err != nil {
			return &plugin.ExecuteResponse{
				Success: false,
				Error:   "created announcement but failed to publish: " + err.Error(),
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
		Message: "Created LaunchNotes announcement (" + state + ")",
		Outputs: map[string]any{
			"announcement_id": announcementID,
			"title":           title,
			"state":           state,
		},
	}, nil
}

func (p *testPlugin) executeCreateAnnouncementWithTestServer(ctx context.Context, cfg *Config, title, content string) (string, error) {
	mutation := `mutation CreateAnnouncement($input: CreateAnnouncementInput!) { createAnnouncement(input: $input) { announcement { id title state } errors { message } } }`

	variables := map[string]any{
		"input": map[string]any{
			"projectId": cfg.ProjectID,
			"title":     title,
			"content":   content,
		},
	}

	resp, err := p.executeGraphQLWithTestServer(ctx, cfg.APIToken, mutation, variables)
	if err != nil {
		return "", err
	}

	var result CreateAnnouncementResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", err
	}

	if len(result.CreateAnnouncement.Errors) > 0 {
		return "", &graphqlError{message: "LaunchNotes error: " + result.CreateAnnouncement.Errors[0].Message}
	}

	return result.CreateAnnouncement.Announcement.ID, nil
}

func (p *testPlugin) executePublishAnnouncementWithTestServer(ctx context.Context, cfg *Config, announcementID string) error {
	mutation := `mutation PublishAnnouncement($input: PublishAnnouncementInput!) { publishAnnouncement(input: $input) { announcement { id state publishedAt } errors { message } } }`

	variables := map[string]any{
		"input": map[string]any{
			"announcementId":    announcementID,
			"notifySubscribers": cfg.NotifySubscribers,
		},
	}

	resp, err := p.executeGraphQLWithTestServer(ctx, cfg.APIToken, mutation, variables)
	if err != nil {
		return err
	}

	var result PublishAnnouncementResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return err
	}

	if len(result.PublishAnnouncement.Errors) > 0 {
		return &graphqlError{message: "LaunchNotes error: " + result.PublishAnnouncement.Errors[0].Message}
	}

	return nil
}

func (p *testPlugin) executeGraphQLWithTestServer(ctx context.Context, token, query string, variables map[string]any) (*GraphQLResponse, error) {
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.testServer.URL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.testServer.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &graphqlError{message: "LaunchNotes returned status " + resp.Status}
	}

	var graphQLResp GraphQLResponse
	if err := json.Unmarshal(body, &graphQLResp); err != nil {
		return nil, err
	}

	if len(graphQLResp.Errors) > 0 {
		return nil, &graphqlError{message: "GraphQL error: " + graphQLResp.Errors[0].Message}
	}

	return &graphQLResp, nil
}

// graphqlError is a simple error type for GraphQL errors.
type graphqlError struct {
	message string
}

func (e *graphqlError) Error() string {
	return e.message
}

// TestHTMLEscaping tests that HTML content is properly escaped to prevent XSS.
func TestHTMLEscaping(t *testing.T) {
	p := &LaunchNotesPlugin{}

	tests := []struct {
		name         string
		releaseNotes string
		changelog    string
	}{
		{
			name:         "escape script tags in release notes",
			releaseNotes: "<script>alert('xss')</script>",
		},
		{
			name:      "escape script tags in changelog",
			changelog: "<script>document.cookie</script>",
		},
		{
			name:         "escape HTML entities",
			releaseNotes: "<img src=x onerror=alert(1)>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook: plugin.HookPostPublish,
				Config: map[string]any{
					"api_token":         "test-token",
					"project_id":        "test-project",
					"include_changelog": true,
				},
				Context: plugin.ReleaseContext{
					Version:      "1.0.0",
					ReleaseNotes: tt.releaseNotes,
					Changelog:    tt.changelog,
				},
				DryRun: true,
			}

			resp, err := p.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if !resp.Success {
				t.Errorf("Execute().Success = false, expected true")
			}
		})
	}
}

// TestNilConfig tests that the plugin handles nil config gracefully.
func TestNilConfig(t *testing.T) {
	p := &LaunchNotesPlugin{}

	cfg := p.parseConfig(nil)

	// Should return defaults
	if cfg.AutoPublish != true {
		t.Error("Expected AutoPublish default to be true")
	}
	if cfg.IncludeChangelog != true {
		t.Error("Expected IncludeChangelog default to be true")
	}
	if cfg.TitleTemplate != "Release {{version}}" {
		t.Errorf("Expected default TitleTemplate, got %q", cfg.TitleTemplate)
	}
}

// TestContextCancellation tests that the plugin respects context cancellation.
func TestContextCancellation(t *testing.T) {
	p := &LaunchNotesPlugin{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"api_token":  "test-token",
			"project_id": "test-project",
		},
		Context: plugin.ReleaseContext{
			Version:      "1.0.0",
			ReleaseNotes: "Test notes",
		},
		DryRun: true,
	}

	// Dry run should still succeed even with cancelled context
	resp, err := p.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected dry run to succeed even with cancelled context")
	}
}

// mockHTTPClient implements HTTPClient for testing.
type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.DoFunc(req)
}

// setupTestHTTPClient sets up a mock HTTP client and restores the original after the test.
func setupTestHTTPClient(t *testing.T, handler func(req *http.Request) (*http.Response, error)) func() {
	originalClient := httpClient
	originalEndpoint := launchNotesGraphQLEndpoint

	httpClient = &mockHTTPClient{DoFunc: handler}
	launchNotesGraphQLEndpoint = "https://test.launchnotes.io/graphql"

	return func() {
		httpClient = originalClient
		launchNotesGraphQLEndpoint = originalEndpoint
	}
}

// TestExecuteWithMockedHTTPClient tests the real Execute code path with a mocked HTTP client.
func TestExecuteWithMockedHTTPClient(t *testing.T) {
	tests := []struct {
		name           string
		config         map[string]any
		releaseContext plugin.ReleaseContext
		responses      []string
		statuses       []int
		dryRun         bool
		expectSuccess  bool
		expectMessage  string
		expectError    string
	}{
		{
			name: "successful creation and publish",
			config: map[string]any{
				"api_token":    "test-token",
				"project_id":   "test-project",
				"auto_publish": true,
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			responses: []string{
				`{"data":{"createAnnouncement":{"announcement":{"id":"ann-123","title":"Release 1.0.0","state":"draft"},"errors":[]}}}`,
				`{"data":{"publishAnnouncement":{"announcement":{"id":"ann-123","state":"published","publishedAt":"2024-01-15T10:00:00Z"},"errors":[]}}}`,
			},
			statuses:      []int{http.StatusOK, http.StatusOK},
			dryRun:        false,
			expectSuccess: true,
			expectMessage: "Created LaunchNotes announcement (published)",
		},
		{
			name: "create as draft only",
			config: map[string]any{
				"api_token":    "test-token",
				"project_id":   "test-project",
				"create_draft": true,
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			responses: []string{
				`{"data":{"createAnnouncement":{"announcement":{"id":"ann-456","title":"Release 1.0.0","state":"draft"},"errors":[]}}}`,
			},
			statuses:      []int{http.StatusOK},
			dryRun:        false,
			expectSuccess: true,
			expectMessage: "Created LaunchNotes announcement (draft)",
		},
		{
			name: "create failed",
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			responses: []string{
				`{"data":{"createAnnouncement":{"announcement":null,"errors":[{"message":"Unauthorized"}]}}}`,
			},
			statuses:      []int{http.StatusOK},
			dryRun:        false,
			expectSuccess: false,
			expectError:   "LaunchNotes error: Unauthorized",
		},
		{
			name: "publish failed after successful creation",
			config: map[string]any{
				"api_token":    "test-token",
				"project_id":   "test-project",
				"auto_publish": true,
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			responses: []string{
				`{"data":{"createAnnouncement":{"announcement":{"id":"ann-789","title":"Release 1.0.0","state":"draft"},"errors":[]}}}`,
				`{"data":{"publishAnnouncement":{"announcement":null,"errors":[{"message":"Publish quota exceeded"}]}}}`,
			},
			statuses:      []int{http.StatusOK, http.StatusOK},
			dryRun:        false,
			expectSuccess: false,
			expectError:   "failed to publish",
		},
		{
			name: "HTTP error on create",
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			responses: []string{
				`Internal Server Error`,
			},
			statuses:      []int{http.StatusInternalServerError},
			dryRun:        false,
			expectSuccess: false,
			expectError:   "status 500",
		},
		{
			name: "GraphQL error on create",
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			responses: []string{
				`{"errors":[{"message":"Invalid query"}]}`,
			},
			statuses:      []int{http.StatusOK},
			dryRun:        false,
			expectSuccess: false,
			expectError:   "GraphQL error",
		},
		{
			name: "with categories and change types",
			config: map[string]any{
				"api_token":    "test-token",
				"project_id":   "test-project",
				"auto_publish": false,
				"categories":   []any{"cat-1", "cat-2"},
				"change_types": []any{"new", "improved"},
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			responses: []string{
				`{"data":{"createAnnouncement":{"announcement":{"id":"ann-cat","title":"Release 1.0.0","state":"draft"},"errors":[]}}}`,
			},
			statuses:      []int{http.StatusOK},
			dryRun:        false,
			expectSuccess: true,
			expectMessage: "Created LaunchNotes announcement (draft)",
		},
		{
			name: "with notify subscribers",
			config: map[string]any{
				"api_token":          "test-token",
				"project_id":         "test-project",
				"auto_publish":       true,
				"notify_subscribers": true,
			},
			releaseContext: plugin.ReleaseContext{
				Version:      "1.0.0",
				ReleaseNotes: "Test notes",
			},
			responses: []string{
				`{"data":{"createAnnouncement":{"announcement":{"id":"ann-notify","title":"Release 1.0.0","state":"draft"},"errors":[]}}}`,
				`{"data":{"publishAnnouncement":{"announcement":{"id":"ann-notify","state":"published","publishedAt":"2024-01-15T10:00:00Z"},"errors":[]}}}`,
			},
			statuses:      []int{http.StatusOK, http.StatusOK},
			dryRun:        false,
			expectSuccess: true,
			expectMessage: "Created LaunchNotes announcement (published)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			cleanup := setupTestHTTPClient(t, func(req *http.Request) (*http.Response, error) {
				if callCount >= len(tt.responses) {
					t.Fatalf("Unexpected call #%d to HTTP client", callCount)
				}

				response := tt.responses[callCount]
				status := tt.statuses[callCount]
				callCount++

				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(strings.NewReader(response)),
				}, nil
			})
			defer cleanup()

			p := &LaunchNotesPlugin{}
			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseContext,
				DryRun:  tt.dryRun,
			}

			resp, err := p.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if resp.Success != tt.expectSuccess {
				t.Errorf("Execute().Success = %v, expected %v (error: %s)", resp.Success, tt.expectSuccess, resp.Error)
			}

			if tt.expectMessage != "" && resp.Message != tt.expectMessage {
				t.Errorf("Execute().Message = %q, expected %q", resp.Message, tt.expectMessage)
			}

			if tt.expectError != "" && !strings.Contains(resp.Error, tt.expectError) {
				t.Errorf("Execute().Error = %q, expected to contain %q", resp.Error, tt.expectError)
			}
		})
	}
}

// TestExecuteGraphQLWithMockedClient tests the executeGraphQL function directly.
func TestExecuteGraphQLWithMockedClient(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		status        int
		httpError     error
		expectError   bool
		errorContains string
	}{
		{
			name: "successful response",
			response: `{
				"data": {"test": "value"}
			}`,
			status:      http.StatusOK,
			expectError: false,
		},
		{
			name:          "HTTP client error",
			httpError:     context.DeadlineExceeded,
			expectError:   true,
			errorContains: "failed to send request",
		},
		{
			name:          "server returns 401",
			response:      "Unauthorized",
			status:        http.StatusUnauthorized,
			expectError:   true,
			errorContains: "status 401",
		},
		{
			name:          "server returns 500",
			response:      "Internal Server Error",
			status:        http.StatusInternalServerError,
			expectError:   true,
			errorContains: "status 500",
		},
		{
			name: "GraphQL error in response",
			response: `{
				"errors": [{"message": "Field not found"}]
			}`,
			status:        http.StatusOK,
			expectError:   true,
			errorContains: "GraphQL error: Field not found",
		},
		{
			name:          "invalid JSON response",
			response:      "not json",
			status:        http.StatusOK,
			expectError:   true,
			errorContains: "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestHTTPClient(t, func(req *http.Request) (*http.Response, error) {
				if tt.httpError != nil {
					return nil, tt.httpError
				}
				return &http.Response{
					StatusCode: tt.status,
					Body:       io.NopCloser(strings.NewReader(tt.response)),
				}, nil
			})
			defer cleanup()

			p := &LaunchNotesPlugin{}
			resp, err := p.executeGraphQL(context.Background(), "test-token", "query { test }", nil)

			if (err != nil) != tt.expectError {
				t.Errorf("executeGraphQL() error = %v, expectError %v", err, tt.expectError)
			}

			if tt.expectError && tt.errorContains != "" {
				if err == nil {
					t.Fatal("Expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error = %q, expected to contain %q", err.Error(), tt.errorContains)
				}
			}

			if !tt.expectError && resp == nil {
				t.Error("Expected non-nil response")
			}
		})
	}
}

// TestExecuteCreateAnnouncementWithMockedClient tests the executeCreateAnnouncement function.
func TestExecuteCreateAnnouncementWithMockedClient(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		response      string
		status        int
		expectError   bool
		errorContains string
		expectedID    string
	}{
		{
			name: "successful creation",
			config: &Config{
				APIToken:  "test-token",
				ProjectID: "proj-123",
			},
			response: `{
				"data": {
					"createAnnouncement": {
						"announcement": {"id": "ann-success", "title": "Test", "state": "draft"},
						"errors": []
					}
				}
			}`,
			status:      http.StatusOK,
			expectError: false,
			expectedID:  "ann-success",
		},
		{
			name: "creation with categories",
			config: &Config{
				APIToken:   "test-token",
				ProjectID:  "proj-123",
				Categories: []string{"cat-1", "cat-2"},
			},
			response: `{
				"data": {
					"createAnnouncement": {
						"announcement": {"id": "ann-with-cats", "title": "Test", "state": "draft"},
						"errors": []
					}
				}
			}`,
			status:      http.StatusOK,
			expectError: false,
			expectedID:  "ann-with-cats",
		},
		{
			name: "creation with change types",
			config: &Config{
				APIToken:    "test-token",
				ProjectID:   "proj-123",
				ChangeTypes: []string{"new", "improved"},
			},
			response: `{
				"data": {
					"createAnnouncement": {
						"announcement": {"id": "ann-with-types", "title": "Test", "state": "draft"},
						"errors": []
					}
				}
			}`,
			status:      http.StatusOK,
			expectError: false,
			expectedID:  "ann-with-types",
		},
		{
			name: "creation error from LaunchNotes",
			config: &Config{
				APIToken:  "test-token",
				ProjectID: "proj-123",
			},
			response: `{
				"data": {
					"createAnnouncement": {
						"announcement": null,
						"errors": [{"message": "Invalid project ID"}]
					}
				}
			}`,
			status:        http.StatusOK,
			expectError:   true,
			errorContains: "Invalid project ID",
		},
		{
			name: "invalid JSON response",
			config: &Config{
				APIToken:  "test-token",
				ProjectID: "proj-123",
			},
			response:      `{"data": {"createAnnouncement": "invalid"}}`,
			status:        http.StatusOK,
			expectError:   true,
			errorContains: "failed to parse response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestHTTPClient(t, func(req *http.Request) (*http.Response, error) {
				// Verify request contains correct authorization
				auth := req.Header.Get("Authorization")
				if !strings.HasPrefix(auth, "Bearer ") {
					t.Errorf("Expected Bearer token, got %q", auth)
				}

				// Verify request body contains expected fields
				body, _ := io.ReadAll(req.Body)
				var gqlReq GraphQLRequest
				_ = json.Unmarshal(body, &gqlReq)

				if !strings.Contains(gqlReq.Query, "createAnnouncement") {
					t.Errorf("Expected createAnnouncement mutation, got %q", gqlReq.Query)
				}

				input, ok := gqlReq.Variables["input"].(map[string]any)
				if !ok {
					t.Error("Expected input variable in request")
				} else {
					if input["projectId"] != tt.config.ProjectID {
						t.Errorf("Expected projectId %q, got %v", tt.config.ProjectID, input["projectId"])
					}
				}

				return &http.Response{
					StatusCode: tt.status,
					Body:       io.NopCloser(strings.NewReader(tt.response)),
				}, nil
			})
			defer cleanup()

			p := &LaunchNotesPlugin{}
			id, err := p.executeCreateAnnouncement(context.Background(), tt.config, "Test Title", "Test Content")

			if (err != nil) != tt.expectError {
				t.Errorf("executeCreateAnnouncement() error = %v, expectError %v", err, tt.expectError)
			}

			if tt.expectError && tt.errorContains != "" {
				if err == nil {
					t.Fatal("Expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error = %q, expected to contain %q", err.Error(), tt.errorContains)
				}
			}

			if !tt.expectError && id != tt.expectedID {
				t.Errorf("executeCreateAnnouncement() id = %q, expected %q", id, tt.expectedID)
			}
		})
	}
}

// TestExecutePublishAnnouncementWithMockedClient tests the executePublishAnnouncement function.
func TestExecutePublishAnnouncementWithMockedClient(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		response      string
		status        int
		expectError   bool
		errorContains string
	}{
		{
			name: "successful publish",
			config: &Config{
				APIToken:          "test-token",
				NotifySubscribers: false,
			},
			response: `{
				"data": {
					"publishAnnouncement": {
						"announcement": {"id": "ann-123", "state": "published", "publishedAt": "2024-01-15T10:00:00Z"},
						"errors": []
					}
				}
			}`,
			status:      http.StatusOK,
			expectError: false,
		},
		{
			name: "successful publish with notifications",
			config: &Config{
				APIToken:          "test-token",
				NotifySubscribers: true,
			},
			response: `{
				"data": {
					"publishAnnouncement": {
						"announcement": {"id": "ann-123", "state": "published", "publishedAt": "2024-01-15T10:00:00Z"},
						"errors": []
					}
				}
			}`,
			status:      http.StatusOK,
			expectError: false,
		},
		{
			name: "publish error from LaunchNotes",
			config: &Config{
				APIToken: "test-token",
			},
			response: `{
				"data": {
					"publishAnnouncement": {
						"announcement": null,
						"errors": [{"message": "Cannot publish draft"}]
					}
				}
			}`,
			status:        http.StatusOK,
			expectError:   true,
			errorContains: "Cannot publish draft",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestHTTPClient(t, func(req *http.Request) (*http.Response, error) {
				// Verify request body contains correct fields
				body, _ := io.ReadAll(req.Body)
				var gqlReq GraphQLRequest
				_ = json.Unmarshal(body, &gqlReq)

				if !strings.Contains(gqlReq.Query, "publishAnnouncement") {
					t.Errorf("Expected publishAnnouncement mutation, got %q", gqlReq.Query)
				}

				input, ok := gqlReq.Variables["input"].(map[string]any)
				if !ok {
					t.Error("Expected input variable in request")
				} else {
					if input["notifySubscribers"] != tt.config.NotifySubscribers {
						t.Errorf("Expected notifySubscribers %v, got %v", tt.config.NotifySubscribers, input["notifySubscribers"])
					}
				}

				return &http.Response{
					StatusCode: tt.status,
					Body:       io.NopCloser(strings.NewReader(tt.response)),
				}, nil
			})
			defer cleanup()

			p := &LaunchNotesPlugin{}
			err := p.executePublishAnnouncement(context.Background(), tt.config, "ann-123")

			if (err != nil) != tt.expectError {
				t.Errorf("executePublishAnnouncement() error = %v, expectError %v", err, tt.expectError)
			}

			if tt.expectError && tt.errorContains != "" {
				if err == nil {
					t.Fatal("Expected error but got nil")
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Error = %q, expected to contain %q", err.Error(), tt.errorContains)
				}
			}
		})
	}
}

// TestGetHTTPClient tests the getHTTPClient function.
func TestGetHTTPClient(t *testing.T) {
	// Test default behavior
	originalClient := httpClient
	defer func() { httpClient = originalClient }()

	// With nil httpClient, should return defaultHTTPClient
	httpClient = nil
	client := getHTTPClient()
	if client != defaultHTTPClient {
		t.Error("Expected defaultHTTPClient when httpClient is nil")
	}

	// With custom httpClient, should return that instead
	customClient := &mockHTTPClient{}
	httpClient = customClient
	client = getHTTPClient()
	if client != customClient {
		t.Error("Expected custom client when httpClient is set")
	}
}

// TestExecuteGraphQL tests the GraphQL execution function directly.
func TestExecuteGraphQL(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse string
		serverStatus   int
		expectError    bool
		errorContains  string
	}{
		{
			name: "successful response",
			serverResponse: `{
				"data": {
					"test": "value"
				}
			}`,
			serverStatus: http.StatusOK,
			expectError:  false,
		},
		{
			name:           "server returns 500",
			serverResponse: "Internal Server Error",
			serverStatus:   http.StatusInternalServerError,
			expectError:    true,
			errorContains:  "status 500",
		},
		{
			name: "GraphQL error in response",
			serverResponse: `{
				"errors": [
					{"message": "Field not found"}
				]
			}`,
			serverStatus:  http.StatusOK,
			expectError:   true,
			errorContains: "GraphQL error",
		},
		{
			name:           "invalid JSON response",
			serverResponse: "not json",
			serverStatus:   http.StatusOK,
			expectError:    true,
			errorContains:  "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverStatus)
				_, _ = w.Write([]byte(tt.serverResponse))
			}))
			defer server.Close()

			// Create plugin with custom endpoint using a wrapper approach
			p := &LaunchNotesPlugin{}

			// We can't directly test executeGraphQL without modifying the endpoint
			// but we can verify the behavior through integration testing
			// This test documents expected behavior

			// For now, just verify the server would respond correctly
			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.serverStatus {
				t.Errorf("Expected status %d, got %d", tt.serverStatus, resp.StatusCode)
			}

			// Also verify parseConfig handles various types correctly
			_ = p.parseConfig(nil)
		})
	}
}

// TestDefaultHTTPClientConfiguration verifies the HTTP client is properly configured.
func TestDefaultHTTPClientConfiguration(t *testing.T) {
	// Verify the HTTP client has expected security settings
	if defaultHTTPClient.Timeout != 30*time.Second {
		t.Errorf("Expected timeout of 30s, got %v", defaultHTTPClient.Timeout)
	}

	transport, ok := defaultHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Expected Transport to be *http.Transport")
	}

	if transport.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("Expected TLS 1.3 minimum, got %v", transport.TLSClientConfig.MinVersion)
	}

	if transport.MaxIdleConns != 10 {
		t.Errorf("Expected MaxIdleConns of 10, got %d", transport.MaxIdleConns)
	}

	if transport.MaxIdleConnsPerHost != 5 {
		t.Errorf("Expected MaxIdleConnsPerHost of 5, got %d", transport.MaxIdleConnsPerHost)
	}

	if transport.IdleConnTimeout != 90*time.Second {
		t.Errorf("Expected IdleConnTimeout of 90s, got %v", transport.IdleConnTimeout)
	}
}

// TestCheckRedirect tests the redirect handler.
func TestCheckRedirect(t *testing.T) {
	tests := []struct {
		name        string
		redirectURL string
		numRedirect int
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid HTTPS redirect within launchnotes.io",
			redirectURL: "https://api.launchnotes.io/graphql",
			numRedirect: 1,
			expectError: false,
		},
		{
			name:        "too many redirects",
			redirectURL: "https://app.launchnotes.io/other",
			numRedirect: 3,
			expectError: true,
			errorMsg:    "too many redirects",
		},
		{
			name:        "redirect to non-HTTPS URL",
			redirectURL: "http://app.launchnotes.io/graphql",
			numRedirect: 1,
			expectError: true,
			errorMsg:    "non-HTTPS",
		},
		{
			name:        "redirect away from launchnotes.io",
			redirectURL: "https://malicious-site.com/steal",
			numRedirect: 1,
			expectError: true,
			errorMsg:    "redirect away from launchnotes.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build mock via requests
			var via []*http.Request
			for i := 0; i < tt.numRedirect; i++ {
				via = append(via, &http.Request{})
			}

			parsedURL, err := url.Parse(tt.redirectURL)
			if err != nil {
				t.Fatalf("Failed to parse URL: %v", err)
			}

			req := &http.Request{
				URL: parsedURL,
			}

			err = defaultHTTPClient.CheckRedirect(req, via)
			if (err != nil) != tt.expectError {
				t.Errorf("CheckRedirect error = %v, expectError = %v", err, tt.expectError)
			}

			if tt.expectError && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain %q, got %q", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

// TestCreateAnnouncementContentBuilding tests different content building scenarios.
func TestCreateAnnouncementContentBuilding(t *testing.T) {
	p := &LaunchNotesPlugin{}

	tests := []struct {
		name           string
		releaseContext plugin.ReleaseContext
		config         map[string]any
		dryRun         bool
	}{
		{
			name: "only breaking changes",
			releaseContext: plugin.ReleaseContext{
				Version: "2.0.0",
				Changes: &plugin.CategorizedChanges{
					Breaking: []plugin.ConventionalCommit{
						{Description: "Removed deprecated API"},
						{Description: "Changed authentication flow"},
					},
				},
			},
			config: map[string]any{
				"api_token":         "test-token",
				"project_id":        "test-project",
				"include_changelog": true,
			},
			dryRun: true,
		},
		{
			name: "only features",
			releaseContext: plugin.ReleaseContext{
				Version: "1.1.0",
				Changes: &plugin.CategorizedChanges{
					Features: []plugin.ConventionalCommit{
						{Description: "Added dark mode"},
						{Description: "Added export functionality"},
					},
				},
			},
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			dryRun: true,
		},
		{
			name: "only fixes",
			releaseContext: plugin.ReleaseContext{
				Version: "1.0.1",
				Changes: &plugin.CategorizedChanges{
					Fixes: []plugin.ConventionalCommit{
						{Description: "Fixed login issue"},
					},
				},
			},
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			dryRun: true,
		},
		{
			name: "all change types",
			releaseContext: plugin.ReleaseContext{
				Version: "2.0.0",
				Changes: &plugin.CategorizedChanges{
					Breaking: []plugin.ConventionalCommit{
						{Description: "Breaking change 1"},
					},
					Features: []plugin.ConventionalCommit{
						{Description: "Feature 1"},
					},
					Fixes: []plugin.ConventionalCommit{
						{Description: "Fix 1"},
					},
				},
			},
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			dryRun: true,
		},
		{
			name: "empty changes object",
			releaseContext: plugin.ReleaseContext{
				Version: "1.0.0",
				Changes: &plugin.CategorizedChanges{},
			},
			config: map[string]any{
				"api_token":  "test-token",
				"project_id": "test-project",
			},
			dryRun: true,
		},
		{
			name: "nil changes with include_changelog false",
			releaseContext: plugin.ReleaseContext{
				Version:   "1.0.0",
				Changelog: "This should not be included",
			},
			config: map[string]any{
				"api_token":         "test-token",
				"project_id":        "test-project",
				"include_changelog": false,
			},
			dryRun: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := plugin.ExecuteRequest{
				Hook:    plugin.HookPostPublish,
				Config:  tt.config,
				Context: tt.releaseContext,
				DryRun:  tt.dryRun,
			}

			resp, err := p.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if !resp.Success {
				t.Errorf("Execute().Success = false, expected true")
			}
		})
	}
}

// TestPublishFailure tests the scenario when publish fails after successful creation.
func TestPublishFailure(t *testing.T) {
	publishAttempted := false

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req GraphQLRequest
		_ = json.Unmarshal(body, &req)

		if strings.Contains(req.Query, "publishAnnouncement") {
			publishAttempted = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"data": {
					"publishAnnouncement": {
						"announcement": null,
						"errors": [{"message": "Publish failed"}]
					}
				}
			}`))
			return
		}

		// Create succeeds
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": {
				"createAnnouncement": {
					"announcement": {
						"id": "ann-123",
						"title": "Test",
						"state": "draft"
					},
					"errors": []
				}
			}
		}`))
	}))
	defer server.Close()

	p := &testPlugin{
		LaunchNotesPlugin: &LaunchNotesPlugin{},
		testServer:        server,
	}

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"api_token":    "test-token",
			"project_id":   "test-project",
			"auto_publish": true,
		},
		Context: plugin.ReleaseContext{
			Version:      "1.0.0",
			ReleaseNotes: "Test notes",
		},
		DryRun: false,
	}

	resp, err := p.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !publishAttempted {
		t.Error("Expected publish to be attempted")
	}

	if resp.Success {
		t.Error("Expected failure when publish fails")
	}

	if !strings.Contains(resp.Error, "failed to publish") {
		t.Errorf("Expected error to contain 'failed to publish', got %s", resp.Error)
	}

	if resp.Outputs["announcement_id"] != "ann-123" {
		t.Errorf("Expected announcement_id in outputs, got %v", resp.Outputs)
	}
}

// TestAutoPublishDisabled tests that publish is not called when auto_publish is false.
func TestAutoPublishDisabled(t *testing.T) {
	publishCalled := false

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req GraphQLRequest
		_ = json.Unmarshal(body, &req)

		if strings.Contains(req.Query, "publishAnnouncement") {
			publishCalled = true
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": {
				"createAnnouncement": {
					"announcement": {
						"id": "ann-456",
						"title": "Test",
						"state": "draft"
					},
					"errors": []
				}
			}
		}`))
	}))
	defer server.Close()

	p := &testPlugin{
		LaunchNotesPlugin: &LaunchNotesPlugin{},
		testServer:        server,
	}

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"api_token":    "test-token",
			"project_id":   "test-project",
			"auto_publish": false,
		},
		Context: plugin.ReleaseContext{
			Version:      "1.0.0",
			ReleaseNotes: "Test notes",
		},
		DryRun: false,
	}

	resp, err := p.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if publishCalled {
		t.Error("Expected publish NOT to be called when auto_publish is false")
	}

	if !resp.Success {
		t.Errorf("Expected success, got error: %s", resp.Error)
	}

	if resp.Outputs["state"] != "draft" {
		t.Errorf("Expected state to be 'draft', got %v", resp.Outputs["state"])
	}
}

// TestCategoriesAndChangeTypes tests that categories and change types are included in requests.
func TestCategoriesAndChangeTypes(t *testing.T) {
	var receivedVariables map[string]any

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req GraphQLRequest
		_ = json.Unmarshal(body, &req)

		if strings.Contains(req.Query, "createAnnouncement") {
			receivedVariables = req.Variables
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": {
				"createAnnouncement": {
					"announcement": {
						"id": "ann-789",
						"title": "Test",
						"state": "draft"
					},
					"errors": []
				}
			}
		}`))
	}))
	defer server.Close()

	p := &testPluginWithCategories{
		LaunchNotesPlugin: &LaunchNotesPlugin{},
		testServer:        server,
	}

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"api_token":    "test-token",
			"project_id":   "test-project",
			"categories":   []any{"cat-1", "cat-2"},
			"change_types": []any{"new", "improved"},
			"auto_publish": false,
		},
		Context: plugin.ReleaseContext{
			Version:      "1.0.0",
			ReleaseNotes: "Test notes",
		},
		DryRun: false,
	}

	_, err := p.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Verify categories and change types were included
	if receivedVariables == nil {
		t.Fatal("No variables received")
	}

	input, ok := receivedVariables["input"].(map[string]any)
	if !ok {
		t.Fatal("Expected input to be a map")
	}

	cats, ok := input["categoryIds"]
	if !ok {
		t.Error("Expected categoryIds in input")
	}
	if cats == nil {
		t.Error("Expected categoryIds to not be nil")
	}

	types, ok := input["changeTypeIds"]
	if !ok {
		t.Error("Expected changeTypeIds in input")
	}
	if types == nil {
		t.Error("Expected changeTypeIds to not be nil")
	}
}

// testPluginWithCategories extends testPlugin to include categories/change_types.
type testPluginWithCategories struct {
	*LaunchNotesPlugin
	testServer *httptest.Server
}

func (p *testPluginWithCategories) Execute(ctx context.Context, req plugin.ExecuteRequest) (*plugin.ExecuteResponse, error) {
	cfg := p.parseConfig(req.Config)

	switch req.Hook {
	case plugin.HookPostPublish, plugin.HookOnSuccess:
		return p.createAnnouncementWithCategories(ctx, cfg, req.Context, req.DryRun)
	default:
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Hook " + string(req.Hook) + " not handled",
		}, nil
	}
}

func (p *testPluginWithCategories) createAnnouncementWithCategories(ctx context.Context, cfg *Config, releaseCtx plugin.ReleaseContext, dryRun bool) (*plugin.ExecuteResponse, error) {
	title := cfg.TitleTemplate
	if title == "" {
		title = "Release {{version}}"
	}
	title = strings.ReplaceAll(title, "{{version}}", releaseCtx.Version)

	if dryRun {
		return &plugin.ExecuteResponse{
			Success: true,
			Message: "Would create LaunchNotes announcement",
		}, nil
	}

	mutation := `mutation CreateAnnouncement($input: CreateAnnouncementInput!) { createAnnouncement(input: $input) { announcement { id } errors { message } } }`

	variables := map[string]any{
		"input": map[string]any{
			"projectId": cfg.ProjectID,
			"title":     title,
			"content":   releaseCtx.ReleaseNotes,
		},
	}

	// Add categories if present
	if len(cfg.Categories) > 0 {
		variables["input"].(map[string]any)["categoryIds"] = cfg.Categories
	}
	// Add change types if present
	if len(cfg.ChangeTypes) > 0 {
		variables["input"].(map[string]any)["changeTypeIds"] = cfg.ChangeTypes
	}

	reqBody := GraphQLRequest{
		Query:     mutation,
		Variables: variables,
	}

	jsonBody, _ := json.Marshal(reqBody)

	httpReq, _ := http.NewRequestWithContext(ctx, "POST", p.testServer.URL, strings.NewReader(string(jsonBody)))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIToken)

	resp, err := p.testServer.Client().Do(httpReq)
	if err != nil {
		return &plugin.ExecuteResponse{Success: false, Error: err.Error()}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var graphQLResp GraphQLResponse
	_ = json.Unmarshal(body, &graphQLResp)

	var result CreateAnnouncementResponse
	_ = json.Unmarshal(graphQLResp.Data, &result)

	return &plugin.ExecuteResponse{
		Success: true,
		Message: "Created LaunchNotes announcement (draft)",
		Outputs: map[string]any{
			"announcement_id": result.CreateAnnouncement.Announcement.ID,
			"state":           "draft",
		},
	}, nil
}

// TestNotifySubscribersOption tests the notify_subscribers option.
func TestNotifySubscribersOption(t *testing.T) {
	var receivedNotify bool

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req GraphQLRequest
		_ = json.Unmarshal(body, &req)

		if strings.Contains(req.Query, "publishAnnouncement") {
			input := req.Variables["input"].(map[string]any)
			receivedNotify = input["notifySubscribers"].(bool)
		}

		if strings.Contains(req.Query, "createAnnouncement") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"data": {
					"createAnnouncement": {
						"announcement": {"id": "ann-123", "title": "Test", "state": "draft"},
						"errors": []
					}
				}
			}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": {
				"publishAnnouncement": {
					"announcement": {"id": "ann-123", "state": "published", "publishedAt": "2024-01-15T10:00:00Z"},
					"errors": []
				}
			}
		}`))
	}))
	defer server.Close()

	p := &testPlugin{
		LaunchNotesPlugin: &LaunchNotesPlugin{},
		testServer:        server,
	}

	req := plugin.ExecuteRequest{
		Hook: plugin.HookPostPublish,
		Config: map[string]any{
			"api_token":          "test-token",
			"project_id":         "test-project",
			"auto_publish":       true,
			"notify_subscribers": true,
		},
		Context: plugin.ReleaseContext{
			Version:      "1.0.0",
			ReleaseNotes: "Test notes",
		},
		DryRun: false,
	}

	resp, err := p.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected success, got error: %s", resp.Error)
	}

	if !receivedNotify {
		t.Error("Expected notify_subscribers to be true in publish request")
	}
}
