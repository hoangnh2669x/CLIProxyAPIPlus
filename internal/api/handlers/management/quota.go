package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/quota"
)

// Quota exceeded toggles
func (h *Handler) GetSwitchProject(c *gin.Context) {
	c.JSON(200, gin.H{"switch-project": h.cfg.QuotaExceeded.SwitchProject})
}
func (h *Handler) PutSwitchProject(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.SwitchProject = v })
}

func (h *Handler) GetSwitchPreviewModel(c *gin.Context) {
	c.JSON(200, gin.H{"switch-preview-model": h.cfg.QuotaExceeded.SwitchPreviewModel})
}
func (h *Handler) PutSwitchPreviewModel(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.SwitchPreviewModel = v })
}

// GetAllPolicies returns all API key policies
func (h *Handler) GetAllPolicies(c *gin.Context) {
	manager := quota.GetManager()
	policies := manager.AllPolicies()

	c.JSON(http.StatusOK, gin.H{
		"policies": policies,
		"count":    len(policies),
	})
}

// GetPolicy returns the policy for a specific API key
func (h *Handler) GetPolicy(c *gin.Context) {
	apiKey := c.Param("key")
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API key is required"})
		return
	}

	manager := quota.GetManager()
	policy := manager.GetPolicy(apiKey)

	if policy == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Policy not found for this API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"api_key": apiKey,
		"policy":  policy,
	})
}

// PutPolicy creates or updates a policy for an API key
func (h *Handler) PutPolicy(c *gin.Context) {
	apiKey := c.Param("key")
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API key is required"})
		return
	}

	var policy config.APIKeyPolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid policy format", "details": err.Error()})
		return
	}

	// Validate policy
	if policy.HasExpiration() {
		expiresAt := policy.ParsedExpiresAt()
		if expiresAt.IsZero() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid expires_at format. Use '2006-01-02' or RFC3339"})
			return
		}
	}

	// Update in-memory config
	h.mu.Lock()
	if h.cfg.APIKeyPolicies == nil {
		h.cfg.APIKeyPolicies = make(map[string]config.APIKeyPolicy)
	}
	h.cfg.APIKeyPolicies[apiKey] = policy
	h.mu.Unlock()

	// Reload policies in quota manager
	quota.GetManager().LoadPolicies(&h.cfg.SDKConfig)

	// Persist to config file
	if !h.persist(c) {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Policy created/updated successfully",
		"api_key": apiKey,
		"policy":  policy,
	})
}

// DeletePolicy removes a policy for an API key
func (h *Handler) DeletePolicy(c *gin.Context) {
	apiKey := c.Param("key")
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API key is required"})
		return
	}

	h.mu.Lock()
	if h.cfg.APIKeyPolicies == nil {
		h.mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"error": "No policies configured"})
		return
	}

	if _, exists := h.cfg.APIKeyPolicies[apiKey]; !exists {
		h.mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"error": "Policy not found for this API key"})
		return
	}

	delete(h.cfg.APIKeyPolicies, apiKey)
	h.mu.Unlock()

	// Reload policies in quota manager
	quota.GetManager().LoadPolicies(&h.cfg.SDKConfig)

	// Persist to config file
	if !h.persist(c) {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Policy deleted successfully",
		"api_key": apiKey,
	})
}

// GetQuotaStatus returns the quota status for an API key
func (h *Handler) GetQuotaStatus(c *gin.Context) {
	apiKey := c.Param("key")
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API key is required"})
		return
	}

	manager := quota.GetManager()
	status := manager.GetQuotaStatus(apiKey)

	c.JSON(http.StatusOK, status)
}

// GetAllUsage returns usage for all API keys
func (h *Handler) GetAllUsage(c *gin.Context) {
	manager := quota.GetManager()
	allUsage := manager.AllUsage()

	c.JSON(http.StatusOK, gin.H{
		"usage": allUsage,
		"count": len(allUsage),
	})
}

// GetUsageByKey returns usage for a specific API key
func (h *Handler) GetUsageByKey(c *gin.Context) {
	apiKey := c.Param("key")
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API key is required"})
		return
	}

	manager := quota.GetManager()
	usage := manager.GetUsageReadOnly(apiKey)

	if usage == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No usage data found for this API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"usage": usage,
	})
}
