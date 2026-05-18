package gateway

import (
	"net/http"
	"strconv"

	"streamgate/pkg/models"
	"streamgate/pkg/service"

	"github.com/gin-gonic/gin"
)

func RegisterCategoryRoutes(router *gin.RouterGroup, svc *service.CategoryService) {
	router.GET("/api/v1/categories", listCategories(svc))
	router.POST("/api/v1/categories", createCategory(svc))
	router.GET("/api/v1/categories/:id", getCategory(svc))
	router.PUT("/api/v1/categories/:id", updateCategory(svc))
	router.DELETE("/api/v1/categories/:id", deleteCategory(svc))
	router.POST("/api/v1/content/:id/categories/:catId", bindContentCategory(svc))
	router.DELETE("/api/v1/content/:id/categories/:catId", unbindContentCategory(svc))
	router.GET("/api/v1/categories/:id/content", listContentByCategory(svc))
}

func listCategories(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		cats, err := svc.ListCategories(c.Request.Context())
		if err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respondOK(c, gin.H{"categories": cats})
	}
}

func createCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name        string `json:"name" binding:"required"`
			Slug        string `json:"slug" binding:"required"`
			Description string `json:"description"`
			ParentID    string `json:"parent_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			respond(c, http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
			return
		}
		cat := &models.Category{
			Name:        req.Name,
			Slug:        req.Slug,
			Description: req.Description,
			ParentID:    req.ParentID,
		}
		id, err := svc.CreateCategory(c.Request.Context(), cat)
		if err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respond(c, http.StatusCreated, gin.H{"id": id, "category": cat})
	}
}

func getCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		cat, err := svc.GetCategory(c.Request.Context(), id)
		if err != nil {
			respond(c, http.StatusNotFound, gin.H{"error": err.Error(), "code": "NOT_FOUND"})
			return
		}
		respondOK(c, cat)
	}
}

func updateCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			Name        string `json:"name"`
			Slug        string `json:"slug"`
			Description string `json:"description"`
			ParentID    string `json:"parent_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			respond(c, http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
			return
		}
		existing, err := svc.GetCategory(c.Request.Context(), id)
		if err != nil {
			respond(c, http.StatusNotFound, gin.H{"error": err.Error(), "code": "NOT_FOUND"})
			return
		}
		if req.Name != "" {
			existing.Name = req.Name
		}
		if req.Slug != "" {
			existing.Slug = req.Slug
		}
		if req.Description != "" {
			existing.Description = req.Description
		}
		if req.ParentID != "" {
			existing.ParentID = req.ParentID
		}
		if err := svc.UpdateCategory(c.Request.Context(), existing); err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respondOK(c, gin.H{"category": existing})
	}
}

func deleteCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := svc.DeleteCategory(c.Request.Context(), id); err != nil {
			respond(c, http.StatusNotFound, gin.H{"error": err.Error(), "code": "NOT_FOUND"})
			return
		}
		respond(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func bindContentCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		catID := c.Param("catId")
		if err := svc.BindContent(c.Request.Context(), contentID, catID); err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respond(c, http.StatusOK, gin.H{"bound": true})
	}
}

func unbindContentCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		catID := c.Param("catId")
		if err := svc.UnbindContent(c.Request.Context(), contentID, catID); err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respond(c, http.StatusOK, gin.H{"unbound": true})
	}
}

func listContentByCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		catID := c.Param("id")
		limit := 20
		offset := 0
		if l := c.Query("limit"); l != "" {
			if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
				limit = parsed
			}
		}
		if o := c.Query("offset"); o != "" {
			if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
				offset = parsed
			}
		}
		ids, err := svc.ListContentByCategory(c.Request.Context(), catID, limit, offset)
		if err != nil {
			respond(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		respondOK(c, gin.H{"content_ids": ids})
	}
}
