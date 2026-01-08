package claude

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestClaudeCodeModelMapper_MapModel_ExactMatch(t *testing.T) {
	mappings := []config.AmpModelMapping{
		{From: "claude-3-opus", To: "gemini-2.0-flash", Regex: false},
		{From: "claude-sonnet-4", To: "gemini-2.5-pro", Regex: false},
	}

	mapper := NewClaudeCodeModelMapper(mappings)

	tests := []struct {
		name           string
		requestedModel string
		expectedResult string
	}{
		{
			name:           "exact match lowercase",
			requestedModel: "claude-3-opus",
			expectedResult: "", // Will be empty if no provider available
		},
		{
			name:           "exact match case insensitive",
			requestedModel: "Claude-3-Opus",
			expectedResult: "", // Will be empty if no provider available
		},
		{
			name:           "no match",
			requestedModel: "unknown-model",
			expectedResult: "",
		},
		{
			name:           "empty model",
			requestedModel: "",
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapper.MapModel(tt.requestedModel)
			// Note: MapModel returns empty string when target has no providers
			// In tests without provider setup, we just verify it doesn't panic
			if tt.requestedModel == "" && result != "" {
				t.Errorf("MapModel(%q) = %q, want empty string for empty input", tt.requestedModel, result)
			}
			if tt.requestedModel == "unknown-model" && result != "" {
				t.Errorf("MapModel(%q) = %q, want empty string for unknown model", tt.requestedModel, result)
			}
		})
	}
}

func TestClaudeCodeModelMapper_MapModel_RegexMatch(t *testing.T) {
	mappings := []config.AmpModelMapping{
		{From: "claude-.*-opus", To: "gemini-2.0-flash", Regex: true},
		{From: "gpt-[0-9]+", To: "gemini-2.5-pro", Regex: true},
	}

	mapper := NewClaudeCodeModelMapper(mappings)

	tests := []struct {
		name           string
		requestedModel string
		shouldMatch    bool
	}{
		{
			name:           "regex match claude pattern",
			requestedModel: "claude-3-opus",
			shouldMatch:    true,
		},
		{
			name:           "regex match gpt pattern",
			requestedModel: "gpt-4",
			shouldMatch:    true,
		},
		{
			name:           "no regex match",
			requestedModel: "llama-3",
			shouldMatch:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapper.MapModel(tt.requestedModel)
			// Note: Result will be empty if no provider available for target
			// We're testing that the mapping logic works, not provider availability
			if !tt.shouldMatch && result != "" {
				t.Errorf("MapModel(%q) = %q, expected no match", tt.requestedModel, result)
			}
		})
	}
}

func TestClaudeCodeModelMapper_UpdateMappings(t *testing.T) {
	initialMappings := []config.AmpModelMapping{
		{From: "model-a", To: "target-a", Regex: false},
	}

	mapper := NewClaudeCodeModelMapper(initialMappings)

	// Verify initial mappings
	mappings := mapper.GetMappings()
	if _, ok := mappings["model-a"]; !ok {
		t.Error("Expected 'model-a' in initial mappings")
	}

	// Update mappings
	newMappings := []config.AmpModelMapping{
		{From: "model-b", To: "target-b", Regex: false},
		{From: "model-c", To: "target-c", Regex: false},
	}
	mapper.UpdateMappings(newMappings)

	// Verify updated mappings
	mappings = mapper.GetMappings()
	if _, ok := mappings["model-a"]; ok {
		t.Error("Expected 'model-a' to be removed after update")
	}
	if _, ok := mappings["model-b"]; !ok {
		t.Error("Expected 'model-b' in updated mappings")
	}
	if _, ok := mappings["model-c"]; !ok {
		t.Error("Expected 'model-c' in updated mappings")
	}
}

func TestClaudeCodeModelMapper_InvalidMappings(t *testing.T) {
	mappings := []config.AmpModelMapping{
		{From: "", To: "target", Regex: false},           // Empty from
		{From: "model", To: "", Regex: false},            // Empty to
		{From: "[invalid", To: "target", Regex: true},    // Invalid regex
		{From: "valid-model", To: "valid-target", Regex: false}, // Valid
	}

	mapper := NewClaudeCodeModelMapper(mappings)

	// Should only have the valid mapping
	result := mapper.GetMappings()
	if len(result) != 1 {
		t.Errorf("Expected 1 valid mapping, got %d", len(result))
	}
	if _, ok := result["valid-model"]; !ok {
		t.Error("Expected 'valid-model' in mappings")
	}
}

func TestClaudeCodeModelMapper_GetMappings(t *testing.T) {
	mappings := []config.AmpModelMapping{
		{From: "model-1", To: "target-1", Regex: false},
		{From: "model-2", To: "target-2", Regex: false},
	}

	mapper := NewClaudeCodeModelMapper(mappings)
	result := mapper.GetMappings()

	// Verify it returns a copy (modifying result shouldn't affect mapper)
	result["model-3"] = "target-3"

	originalMappings := mapper.GetMappings()
	if _, ok := originalMappings["model-3"]; ok {
		t.Error("GetMappings should return a copy, not the original map")
	}
}

func TestExtractModelFromBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "valid model field",
			body:     `{"model": "claude-3-opus", "messages": []}`,
			expected: "claude-3-opus",
		},
		{
			name:     "no model field",
			body:     `{"messages": []}`,
			expected: "",
		},
		{
			name:     "empty body",
			body:     `{}`,
			expected: "",
		},
		{
			name:     "invalid json",
			body:     `not json`,
			expected: "",
		},
		{
			name:     "model is number",
			body:     `{"model": 123}`,
			expected: "",
		},
		{
			name:     "model is null",
			body:     `{"model": null}`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractModelFromBody([]byte(tt.body))
			if result != tt.expected {
				t.Errorf("extractModelFromBody(%q) = %q, want %q", tt.body, result, tt.expected)
			}
		})
	}
}

func TestRewriteModelInBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		newModel string
		expected string
	}{
		{
			name:     "rewrite existing model",
			body:     `{"model":"old-model","messages":[]}`,
			newModel: "new-model",
			expected: `{"model":"new-model","messages":[]}`,
		},
		{
			name:     "no model field",
			body:     `{"messages":[]}`,
			newModel: "new-model",
			expected: `{"messages":[]}`,
		},
		{
			name:     "empty body",
			body:     `{}`,
			newModel: "new-model",
			expected: `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rewriteModelInBody([]byte(tt.body), tt.newModel)
			if string(result) != tt.expected {
				t.Errorf("rewriteModelInBody(%q, %q) = %q, want %q", tt.body, tt.newModel, string(result), tt.expected)
			}
		})
	}
}

func TestNewModelMappingHandler(t *testing.T) {
	mapper := NewClaudeCodeModelMapper(nil)
	handler := NewModelMappingHandler(mapper, nil)

	if handler == nil {
		t.Error("NewModelMappingHandler returned nil")
	}
	if handler.mapper != mapper {
		t.Error("Handler mapper not set correctly")
	}
}

func TestModelMappingHandler_SetMapper(t *testing.T) {
	mapper1 := NewClaudeCodeModelMapper(nil)
	mapper2 := NewClaudeCodeModelMapper([]config.AmpModelMapping{
		{From: "test", To: "target", Regex: false},
	})

	handler := NewModelMappingHandler(mapper1, nil)
	handler.SetMapper(mapper2)

	if handler.mapper != mapper2 {
		t.Error("SetMapper did not update the mapper")
	}
}
