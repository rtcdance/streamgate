package gateway

import (
	"net/http"
	"strconv"

	"github.com/rtcdance/streamgate/pkg/models"
	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/gin-gonic/gin"
)

func RegisterCategoryRoutes(router *gin.RouterGroup, svc *service.CategoryService) {
	router.GET(APIPrefix+"/categories", listCategories(svc))
	router.POST(APIPrefix+"/categories", createCategory(svc))
	router.GET(APIPrefix+"/categories/:id", getCategory(svc))
	router.PUT(APIPrefix+"/categories/:id", updateCategory(svc))
	router.DELETE(APIPrefix+"/categories/:id", deleteCategory(svc))
	router.POST(APIPrefix+"/content/:id/categories/:catId", bindContentCategory(svc))
	router.DELETE(APIPrefix+"/content/:id/categories/:catId", unbindContentCategory(svc))
	router.GET(APIPrefix+"/categories/:id/content", listContentByCategory(svc))
}

func listCategories(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		cats, err := svc.ListCategories(c.Request.Context())
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
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
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request body", err.Error())
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
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondCreated(c, gin.H{"id": id, "category": cat})
	}
}

func getCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		cat, err := svc.GetCategory(c.Request.Context(), id)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "category not found")
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
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request body", err.Error())
			return
		}
		existing, err := svc.GetCategory(c.Request.Context(), id)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "category not found")
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
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"category": existing})
	}
}

func deleteCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := svc.DeleteCategory(c.Request.Context(), id); err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "category not found")
			return
		}
		respondOK(c, gin.H{"deleted": true})
	}
}

func bindContentCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		catID := c.Param("catId")
		if err := svc.BindContent(c.Request.Context(), contentID, catID); err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"bound": true})
	}
}

func unbindContentCategory(svc *service.CategoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		contentID := c.Param("id")
		catID := c.Param("catId")
		if err := svc.UnbindContent(c.Request.Context(), contentID, catID); err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"unbound": true})
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
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{"content_ids": ids})
	}
}
