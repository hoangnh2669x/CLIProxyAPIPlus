// Package claude provides model mapping functionality for routing Claude Code CLI requests
// to alternative models when the requested model is not available locally.
package claude

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// MappedModelContextKey is the Gin context key for passing mapped model names.
const MappedModelContextKey = "claudecode_mapped_model"

// ClaudeCodeRouteType represents the type of routing decision made for a Claude Code request
type ClaudeCodeRouteType string

const (
	// RouteTypeLocalProvider indicates the request is handled by a local OAuth provider (free)
	RouteTypeLocalProvider ClaudeCodeRouteType = "LOCAL_PROVIDER"
	// RouteTypeModelMapping indicates the request was remapped to another available model (free)
	RouteTypeModelMapping ClaudeCodeRouteType = "MODEL_MAPPING"
	// RouteTypeNoProvider indicates no provider available
	RouteTypeNoProvider ClaudeCodeRouteType = "NO_PROVIDER"
)

// regexMapping holds a compiled regex and its target model
type regexMapping struct {
	re *regexp.Regexp
	to string
}

// ClaudeCodeModelMapper provides model name mapping/aliasing for Claude Code CLI requests.
// When a Claude Code request comes in for a model that isn't available locally,
// this mapper can redirect it to an alternative model that IS available.
type ClaudeCodeModelMapper struct {
	mu       sync.RWMutex
	mappings map[string]string // exact: from -> to (normalized lowercase keys)
	regexps  []regexMapping    // regex rules evaluated in order
}

// NewClaudeCodeModelMapper creates a new model mapper with the given initial mappings.
func NewClaudeCodeModelMapper(mappings []config.AmpModelMapping) *ClaudeCodeModelMapper {
	m := &ClaudeCodeModelMapper{
		mappings: make(map[string]string),
		regexps:  nil,
	}
	m.UpdateMappings(mappings)
	return m
}

// MapModel checks if a mapping exists for the requested model and if the
// target model has available local providers. Returns the mapped model name
// or empty string if no valid mapping exists.
func (m *ClaudeCodeModelMapper) MapModel(requestedModel string) string {
	if requestedModel == "" {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Normalize the requested model for lookup
	normalizedRequest := strings.ToLower(strings.TrimSpace(requestedModel))

	// Check for direct mapping
	targetModel, exists := m.mappings[normalizedRequest]
	if !exists {
		// Try regex mappings in order
		base, _ := util.NormalizeThinkingModel(requestedModel)
		for _, rm := range m.regexps {
			if rm.re.MatchString(requestedModel) || (base != "" && rm.re.MatchString(base)) {
				targetModel = rm.to
				exists = true
				break
			}
		}
		if !exists {
			return ""
		}
	}

	// Verify target model has available providers
	normalizedTarget, _ := util.NormalizeThinkingModel(targetModel)
	providers := util.GetProviderName(normalizedTarget)
	if len(providers) == 0 {
		log.Debugf("claudecode model mapping: target model %s has no available providers, skipping mapping", targetModel)
		return ""
	}

	return targetModel
}

// UpdateMappings refreshes the mapping configuration from config.
// This is called during initialization and on config hot-reload.
func (m *ClaudeCodeModelMapper) UpdateMappings(mappings []config.AmpModelMapping) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear and rebuild mappings
	m.mappings = make(map[string]string, len(mappings))
	m.regexps = make([]regexMapping, 0, len(mappings))

	for _, mapping := range mappings {
		from := strings.TrimSpace(mapping.From)
		to := strings.TrimSpace(mapping.To)

		if from == "" || to == "" {
			log.Warnf("claudecode model mapping: skipping invalid mapping (from=%q, to=%q)", from, to)
			continue
		}

		if mapping.Regex {
			// Compile case-insensitive regex; wrap with (?i) to match behavior of exact lookups
			pattern := "(?i)" + from
			re, err := regexp.Compile(pattern)
			if err != nil {
				log.Warnf("claudecode model mapping: invalid regex %q: %v", from, err)
				continue
			}
			m.regexps = append(m.regexps, regexMapping{re: re, to: to})
			log.Debugf("claudecode model regex mapping registered: /%s/ -> %s", from, to)
		} else {
			// Store with normalized lowercase key for case-insensitive lookup
			normalizedFrom := strings.ToLower(from)
			m.mappings[normalizedFrom] = to
			log.Debugf("claudecode model mapping registered: %s -> %s", from, to)
		}
	}

	if len(m.mappings) > 0 {
		log.Infof("claudecode model mapping: loaded %d mapping(s)", len(m.mappings))
	}
	if n := len(m.regexps); n > 0 {
		log.Infof("claudecode model mapping: loaded %d regex mapping(s)", n)
	}
}

// GetMappings returns a copy of current mappings (for debugging/status).
func (m *ClaudeCodeModelMapper) GetMappings() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]string, len(m.mappings))
	for k, v := range m.mappings {
		result[k] = v
	}
	return result
}

// ModelMappingHandler wraps handlers with model mapping logic for Claude Code routes
type ModelMappingHandler struct {
	mu                 sync.RWMutex
	mapper             *ClaudeCodeModelMapper
	forceModelMappings func() bool
}

// NewModelMappingHandler creates a new handler wrapper with the given mapper
func NewModelMappingHandler(mapper *ClaudeCodeModelMapper, forceModelMappings func() bool) *ModelMappingHandler {
	if forceModelMappings == nil {
		forceModelMappings = func() bool { return false }
	}
	return &ModelMappingHandler{
		mapper:             mapper,
		forceModelMappings: forceModelMappings,
	}
}

// SetMapper updates the mapper (for hot-reload support)
func (h *ModelMappingHandler) SetMapper(mapper *ClaudeCodeModelMapper) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mapper = mapper
}

// logClaudeCodeRouting logs the routing decision for a Claude Code request with structured fields
func logClaudeCodeRouting(routeType ClaudeCodeRouteType, requestedModel, resolvedModel, provider, path string) {
	fields := log.Fields{
		"component":       "claudecode-routing",
		"route_type":      string(routeType),
		"requested_model": requestedModel,
		"path":            path,
		"timestamp":       time.Now().Format(time.RFC3339),
	}

	if resolvedModel != "" && resolvedModel != requestedModel {
		fields["resolved_model"] = resolvedModel
	}
	if provider != "" {
		fields["provider"] = provider
	}

	switch routeType {
	case RouteTypeLocalProvider:
		fields["cost"] = "free"
		fields["source"] = "local_oauth"
		log.WithFields(fields).Debugf("claudecode using local provider for model: %s", requestedModel)

	case RouteTypeModelMapping:
		fields["cost"] = "free"
		fields["source"] = "local_oauth"
		fields["mapping"] = requestedModel + " -> " + resolvedModel
		log.WithFields(fields).Debugf("claudecode model mapping: %s -> %s", requestedModel, resolvedModel)

	case RouteTypeNoProvider:
		fields["cost"] = "none"
		fields["source"] = "error"
		fields["model_id"] = requestedModel
		log.WithFields(fields).Warnf("claudecode: no provider available for model_id: %s", requestedModel)
	}
}

// Wrap wraps a gin.HandlerFunc with model mapping logic
func (h *ModelMappingHandler) Wrap(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get mapper reference under lock to avoid race condition with SetMapper
		h.mu.RLock()
		mapper := h.mapper
		forceModelMappings := h.forceModelMappings
		h.mu.RUnlock()

		if mapper == nil {
			handler(c)
			return
		}

		// Only apply model mapping for Claude CLI requests
		userAgent := c.GetHeader("User-Agent")
		if !strings.HasPrefix(userAgent, "claude-cli") {
			handler(c)
			return
		}

		requestPath := c.Request.URL.Path

		// Log Claude CLI request
		log.WithFields(log.Fields{
			"component":  "claudecode-routing",
			"user_agent": userAgent,
			"path":       requestPath,
		}).Info("Claude CLI request received")

		// Read the request body to extract the model name
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			log.Errorf("claudecode mapping: failed to read request body: %v", err)
			handler(c)
			return
		}

		// Restore the body for the handler to read
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Extract model from request body
		modelName := extractModelFromBody(bodyBytes)
		if modelName == "" {
			// Can't determine model, proceed with normal handler
			handler(c)
			return
		}

		// Log requested model from Claude CLI
		log.WithFields(log.Fields{
			"component":        "claudecode-routing",
			"requested_model":  modelName,
			"user_agent":       userAgent,
			"path":             requestPath,
		}).Infof("Claude CLI requesting model: %s", modelName)

		// Normalize model (handles dynamic thinking suffixes)
		normalizedModel, thinkingMetadata := util.NormalizeThinkingModel(modelName)
		thinkingSuffix := ""
		if thinkingMetadata != nil && strings.HasPrefix(modelName, normalizedModel) {
			thinkingSuffix = modelName[len(normalizedModel):]
		}

		// Helper function to resolve mapped model
		resolveMappedModel := func() (string, []string) {
			mappedModel := mapper.MapModel(modelName)
			if mappedModel == "" {
				mappedModel = mapper.MapModel(normalizedModel)
			}
			mappedModel = strings.TrimSpace(mappedModel)
			if mappedModel == "" {
				return "", nil
			}

			// Preserve dynamic thinking suffix when mapping applies
			if thinkingSuffix != "" {
				_, mappedThinkingMetadata := util.NormalizeThinkingModel(mappedModel)
				if mappedThinkingMetadata == nil {
					mappedModel += thinkingSuffix
				}
			}

			mappedBaseModel, _ := util.NormalizeThinkingModel(mappedModel)
			mappedProviders := util.GetProviderName(mappedBaseModel)
			if len(mappedProviders) == 0 {
				return "", nil
			}

			return mappedModel, mappedProviders
		}

		// Track resolved model for logging (may change if mapping is applied)
		resolvedModel := normalizedModel
		usedMapping := false
		var providers []string

		// Check if model mappings should be forced ahead of local API keys
		forceMappings := forceModelMappings != nil && forceModelMappings()

		if forceMappings {
			// FORCE MODE: Check model mappings FIRST (takes precedence over local API keys)
			// This allows users to route Claude Code requests to their preferred OAuth providers
			if mappedModel, mappedProviders := resolveMappedModel(); mappedModel != "" {
				// Mapping found and provider available - rewrite the model in request body
				bodyBytes = rewriteModelInBody(bodyBytes, mappedModel)
				c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				// Store mapped model in context
				c.Set(MappedModelContextKey, mappedModel)
				resolvedModel = mappedModel
				usedMapping = true
				providers = mappedProviders
			}

			// If no mapping applied, check for local providers
			if !usedMapping {
				providers = util.GetProviderName(normalizedModel)
			}
		} else {
			// DEFAULT MODE: Check local providers first, then mappings as fallback
			providers = util.GetProviderName(normalizedModel)

			if len(providers) == 0 {
				// No providers configured - check if we have a model mapping
				if mappedModel, mappedProviders := resolveMappedModel(); mappedModel != "" {
					// Mapping found and provider available - rewrite the model in request body
					bodyBytes = rewriteModelInBody(bodyBytes, mappedModel)
					c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
					// Store mapped model in context
					c.Set(MappedModelContextKey, mappedModel)
					resolvedModel = mappedModel
					usedMapping = true
					providers = mappedProviders
				}
			}
		}

		// Log the routing decision
		providerName := ""
		if len(providers) > 0 {
			providerName = providers[0]
		}

		if usedMapping {
			// Log: Model was mapped to another model
			log.Debugf("claudecode model mapping: request %s -> %s", normalizedModel, resolvedModel)
			logClaudeCodeRouting(RouteTypeModelMapping, modelName, resolvedModel, providerName, requestPath)

			// Use response rewriter to restore original model name in response
			rewriter := NewClaudeCodeResponseRewriter(c.Writer, modelName)
			c.Writer = rewriter
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			handler(c)
			rewriter.Flush()
		} else if len(providers) > 0 {
			// Log: Using local provider (free)
			logClaudeCodeRouting(RouteTypeLocalProvider, modelName, resolvedModel, providerName, requestPath)
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			handler(c)
		} else {
			// No provider and no valid mapping
			logClaudeCodeRouting(RouteTypeNoProvider, modelName, "", "", requestPath)
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			handler(c)
		}
	}
}

// extractModelFromBody extracts the model name from a JSON request body
func extractModelFromBody(body []byte) string {
	if result := gjson.GetBytes(body, "model"); result.Exists() && result.Type == gjson.String {
		return result.String()
	}
	return ""
}

// rewriteModelInBody replaces the model name in a JSON request body
func rewriteModelInBody(body []byte, newModel string) []byte {
	if !gjson.GetBytes(body, "model").Exists() {
		return body
	}
	result, err := sjson.SetBytes(body, "model", newModel)
	if err != nil {
		log.Warnf("claudecode model mapping: failed to rewrite model in request body: %v", err)
		return body
	}
	return result
}
