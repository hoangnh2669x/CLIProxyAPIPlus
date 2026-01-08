package management

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// validateModelMappings validates the model mappings before saving to config.
// It checks that 'from' and 'to' fields are not empty, and if regex is true,
// validates that the 'from' pattern compiles successfully.
func validateModelMappings(mappings []config.AmpModelMapping) error {
	for i, m := range mappings {
		from := strings.TrimSpace(m.From)
		to := strings.TrimSpace(m.To)
		if from == "" {
			return fmt.Errorf("mapping[%d]: 'from' field is required", i)
		}
		if to == "" {
			return fmt.Errorf("mapping[%d]: 'to' field is required", i)
		}
		if m.Regex {
			if _, err := regexp.Compile(from); err != nil {
				return fmt.Errorf("mapping[%d]: invalid regex pattern '%s': %v", i, from, err)
			}
		}
	}
	return nil
}

// GetClaudeCode returns the complete claudecode configuration.
func (h *Handler) GetClaudeCode(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(200, gin.H{"claudecode": config.ClaudeCodeConfig{}})
		return
	}
	c.JSON(200, gin.H{"claudecode": h.cfg.ClaudeCode})
}

// GetClaudeCodeModelMappings returns the claudecode model mappings.
func (h *Handler) GetClaudeCodeModelMappings(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(200, gin.H{"model-mappings": []config.AmpModelMapping{}})
		return
	}
	c.JSON(200, gin.H{"model-mappings": h.cfg.ClaudeCode.ModelMappings})
}

// PutClaudeCodeModelMappings replaces all claudecode model mappings.
func (h *Handler) PutClaudeCodeModelMappings(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(500, gin.H{"error": "config not available"})
		return
	}
	var body struct {
		Value []config.AmpModelMapping `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := validateModelMappings(body.Value); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	h.cfg.ClaudeCode.ModelMappings = body.Value
	h.persist(c)
}

// PatchClaudeCodeModelMappings adds or updates model mappings.
func (h *Handler) PatchClaudeCodeModelMappings(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(500, gin.H{"error": "config not available"})
		return
	}
	var body struct {
		Value []config.AmpModelMapping `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	if err := validateModelMappings(body.Value); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	existing := make(map[string]int)
	for i, m := range h.cfg.ClaudeCode.ModelMappings {
		existing[strings.TrimSpace(m.From)] = i
	}

	for _, newMapping := range body.Value {
		from := strings.TrimSpace(newMapping.From)
		if idx, ok := existing[from]; ok {
			h.cfg.ClaudeCode.ModelMappings[idx] = newMapping
		} else {
			h.cfg.ClaudeCode.ModelMappings = append(h.cfg.ClaudeCode.ModelMappings, newMapping)
			existing[from] = len(h.cfg.ClaudeCode.ModelMappings) - 1
		}
	}
	h.persist(c)
}

// DeleteClaudeCodeModelMappings removes specified model mappings by "from" field.
func (h *Handler) DeleteClaudeCodeModelMappings(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(500, gin.H{"error": "config not available"})
		return
	}
	var body struct {
		Value []string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Value) == 0 {
		h.cfg.ClaudeCode.ModelMappings = nil
		h.persist(c)
		return
	}

	toRemove := make(map[string]bool)
	for _, from := range body.Value {
		toRemove[strings.TrimSpace(from)] = true
	}

	newMappings := make([]config.AmpModelMapping, 0, len(h.cfg.ClaudeCode.ModelMappings))
	for _, m := range h.cfg.ClaudeCode.ModelMappings {
		if !toRemove[strings.TrimSpace(m.From)] {
			newMappings = append(newMappings, m)
		}
	}
	h.cfg.ClaudeCode.ModelMappings = newMappings
	h.persist(c)
}

// GetClaudeCodeForceModelMappings returns whether model mappings are forced.
func (h *Handler) GetClaudeCodeForceModelMappings(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(200, gin.H{"force-model-mappings": false})
		return
	}
	c.JSON(200, gin.H{"force-model-mappings": h.cfg.ClaudeCode.ForceModelMappings})
}

// PutClaudeCodeForceModelMappings updates the force model mappings setting.
func (h *Handler) PutClaudeCodeForceModelMappings(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.ClaudeCode.ForceModelMappings = v })
}
