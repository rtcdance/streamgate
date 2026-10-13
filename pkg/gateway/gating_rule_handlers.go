package gateway

import (
	"errors"
	"net/http"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/service/gating"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// respondGatingRuleError maps an ownership-aware mutation failure onto a stable
// status code: 404 when the resource is absent, 403 when the caller does not
// own it, 500 for anything else. It never echoes the underlying SQL error.
func respondGatingRuleError(c *gin.Context, err error, notFoundMsg string) {
	switch {
	case errors.Is(err, gating.ErrRuleNotFound), errors.Is(err, gating.ErrContentNotFound):
		abortWithError(c, http.StatusNotFound, ErrNotFound, notFoundMsg)
	case errors.Is(err, gating.ErrRuleForbidden):
		abortWithError(c, http.StatusForbidden, ErrForbidden, "you do not own this content")
	default:
		abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
	}
}

func RegisterGatingRuleRoutes(router *gin.RouterGroup, svc *service.GatingRuleService, web3Svc *service.Web3Service, log *zap.Logger) {
	router.GET(APIPrefix+"/content/:id/gating-rules", listGatingRules(svc))
	router.POST(APIPrefix+"/content/:id/gating-rules", createGatingRule(svc, web3Svc, log))
	router.PUT(APIPrefix+"/gating-rules/:ruleId", updateGatingRule(svc, web3Svc, log))
	router.DELETE(APIPrefix+"/gating-rules/:ruleId", deleteGatingRule(svc, web3Svc, log))
}

// refreshEventFilter re-subscribes the NFT event indexer to the new contract
// whitelist after a rule write (SG-13 B). A failure is logged, never surfaced
// to the caller: the rule itself was committed, and the gate reads rules
// directly — the event path only accelerates cache invalidation, so losing it
// degrades freshness, not correctness.
func refreshEventFilter(c *gin.Context, web3Svc *service.Web3Service, log *zap.Logger) {
	if web3Svc == nil {
		return
	}
	if err := web3Svc.RefreshEventFilter(c.Request.Context()); err != nil {
		log.Warn("Event filter refresh after gating-rule write failed",
			zap.String("path", c.Request.URL.Path),
			zap.Error(err))
	}
}

func listGatingRules(svc *service.GatingRuleService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		rules, err := svc.ListRulesByContent(c.Request.Context(), contentID)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"rules": rules})
	}
}

func createGatingRule(svc *service.GatingRuleService, web3Svc *service.Web3Service, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		actor := middleware.GetWalletAddress(c)
		if actor == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet address required")
			return
		}
		var req struct {
			ContractAddress string `json:"contract_address" binding:"required"`
			TokenID         string `json:"token_id"`
			ChainID         int64  `json:"chain_id"`
			Standard        string `json:"standard"`
			MinBalance      int    `json:"min_balance"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request body", err.Error())
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
		// The owner check lives in the service, inside the same statement as the
		// insert: a rule is a write by the content's owner, not by any
		// authenticated caller.
		id, err := svc.CreateRuleForOwner(c.Request.Context(), rule, actor)
		if err != nil {
			respondGatingRuleError(c, err, "content not found")
			return
		}
		refreshEventFilter(c, web3Svc, log)
		respondCreated(c, gin.H{"id": id, "rule": rule})
	}
}

func updateGatingRule(svc *service.GatingRuleService, web3Svc *service.Web3Service, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ruleID := c.Param("ruleId")
		actor := middleware.GetWalletAddress(c)
		if actor == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet address required")
			return
		}
		var req struct {
			ContractAddress string `json:"contract_address"`
			TokenID         string `json:"token_id"`
			ChainID         int64  `json:"chain_id"`
			Standard        string `json:"standard"`
			MinBalance      int    `json:"min_balance"`
			IsActive        *bool  `json:"is_active"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request body", err.Error())
			return
		}
		existing, err := svc.GetRule(c.Request.Context(), ruleID)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "gating rule not found")
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
		// Ownership is re-checked inside the UPDATE itself, so a rule that is
		// retargeted to another content between the read and the write cannot
		// slip past the owner check.
		if err := svc.UpdateRuleForOwner(c.Request.Context(), existing, actor); err != nil {
			respondGatingRuleError(c, err, "gating rule not found")
			return
		}
		refreshEventFilter(c, web3Svc, log)
		respondOK(c, gin.H{"rule": existing})
	}
}

func deleteGatingRule(svc *service.GatingRuleService, web3Svc *service.Web3Service, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ruleID := c.Param("ruleId")
		actor := middleware.GetWalletAddress(c)
		if actor == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet address required")
			return
		}
		if err := svc.DeleteRuleForOwner(c.Request.Context(), ruleID, actor); err != nil {
			respondGatingRuleError(c, err, "gating rule not found")
			return
		}
		refreshEventFilter(c, web3Svc, log)
		respondOK(c, gin.H{"deleted": true})
	}
}
