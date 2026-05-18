package gateway

import (
	"net/http"

	"streamgate/pkg/models"
	"streamgate/pkg/service"

	"github.com/gin-gonic/gin"
)

func RegisterGatingRuleRoutes(router *gin.RouterGroup, svc *service.GatingRuleService) {
	router.GET("/api/v1/content/:id/gating-rules", listGatingRules(svc))
	router.POST("/api/v1/content/:id/gating-rules", createGatingRule(svc))
	router.PUT("/api/v1/gating-rules/:ruleId", updateGatingRule(svc))
	router.DELETE("/api/v1/gating-rules/:ruleId", deleteGatingRule(svc))
}

func listGatingRules(svc *service.GatingRuleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		rules, err := svc.ListRulesByContent(c.Request.Context(), contentID)
		if err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respondOK(c, gin.H{"rules": rules})
	}
}

func createGatingRule(svc *service.GatingRuleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		var req struct {
			ContractAddress string `json:"contract_address" binding:"required"`
			TokenID         string `json:"token_id"`
			ChainID         int64  `json:"chain_id"`
			Standard        string `json:"standard"`
			MinBalance      int    `json:"min_balance"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			respond(c, http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
			return
		}
		rule := &models.GatingRule{
			ContentID:       contentID,
			ContractAddress: req.ContractAddress,
			TokenID:         req.TokenID,
			ChainID:         req.ChainID,
			Standard:        req.Standard,
			MinBalance:      req.MinBalance,
			IsActive:        true,
		}
		id, err := svc.CreateRule(c.Request.Context(), rule)
		if err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respond(c, http.StatusCreated, gin.H{"id": id, "rule": rule})
	}
}

func updateGatingRule(svc *service.GatingRuleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ruleID := c.Param("ruleId")
		var req struct {
			ContractAddress string `json:"contract_address"`
			TokenID         string `json:"token_id"`
			ChainID         int64  `json:"chain_id"`
			Standard        string `json:"standard"`
			MinBalance      int    `json:"min_balance"`
			IsActive        *bool  `json:"is_active"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			respond(c, http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
			return
		}
		existing, err := svc.GetRule(c.Request.Context(), ruleID)
		if err != nil {
			respond(c, http.StatusNotFound, gin.H{"error": err.Error(), "code": "NOT_FOUND"})
			return
		}
		if req.ContractAddress != "" {
			existing.ContractAddress = req.ContractAddress
		}
		if req.TokenID != "" {
			existing.TokenID = req.TokenID
		}
		if req.ChainID != 0 {
			existing.ChainID = req.ChainID
		}
		if req.Standard != "" {
			existing.Standard = req.Standard
		}
		if req.MinBalance > 0 {
			existing.MinBalance = req.MinBalance
		}
		if req.IsActive != nil {
			existing.IsActive = *req.IsActive
		}
		if err := svc.UpdateRule(c.Request.Context(), existing); err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respondOK(c, gin.H{"rule": existing})
	}
}

func deleteGatingRule(svc *service.GatingRuleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ruleID := c.Param("ruleId")
		if err := svc.DeleteRule(c.Request.Context(), ruleID); err != nil {
			respond(c, http.StatusNotFound, gin.H{"error": err.Error(), "code": "NOT_FOUND"})
			return
		}
		respond(c, http.StatusOK, gin.H{"deleted": true})
	}
}
