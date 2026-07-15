package posts

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Module wires post, category, and tag routes into the API.
type Module struct {
	svc *Service
}

// NewModule creates a posts module.
func NewModule(cfg *config.Config, repo *Repository) *Module {
	return &Module{svc: NewService(cfg, repo)}
}

// Register mounts all routes under /api/v1.
func (m *Module) Register(r *gin.Engine, authMW gin.HandlerFunc, adminMW gin.HandlerFunc) {
	v1 := r.Group("/api/v1")

	v1.GET("/posts", m.list)
	v1.GET("/posts/:slug", m.get)
	v1.POST("/posts", authMW, m.create)
	v1.PUT("/posts/:slug", authMW, m.update)
	v1.DELETE("/posts/:slug", authMW, m.delete)

	v1.GET("/categories", m.listCategories)
	v1.POST("/categories", adminMW, m.createCategory)
	v1.PUT("/categories/:slug", adminMW, m.updateCategory)
	v1.DELETE("/categories/:slug", adminMW, m.deleteCategory)

	v1.GET("/tags", m.listTags)
	v1.POST("/tags", adminMW, m.createTag)
	v1.PUT("/tags/:slug", adminMW, m.updateTag)
	v1.DELETE("/tags/:slug", adminMW, m.deleteTag)
}

// --- Post handlers ---

func (m *Module) list(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := m.svc.List(c, page, size)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, result)
}

func (m *Module) get(c *gin.Context) {
	post, err := m.svc.GetBySlug(c, c.Param("slug"))
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{"post": post})
}

func (m *Module) create(c *gin.Context) {
	var input CreatePostInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	post, err := m.svc.Create(c, input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.Created(c, gin.H{"post": post})
}

func (m *Module) update(c *gin.Context) {
	var input UpdatePostInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	post, err := m.svc.Update(c, c.Param("slug"), input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{"post": post})
}

func (m *Module) delete(c *gin.Context) {
	if err := m.svc.Delete(c, c.Param("slug")); err != nil {
		c.Error(err)
		return
	}
	httpserver.NoContent(c)
}

// --- Category handlers ---

func (m *Module) listCategories(c *gin.Context) {
	cats, err := m.svc.ListCategories(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{"categories": cats})
}

func (m *Module) createCategory(c *gin.Context) {
	var input CategoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	cat, err := m.svc.CreateCategory(c.Request.Context(), input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.Created(c, gin.H{"category": cat})
}

func (m *Module) updateCategory(c *gin.Context) {
	var input CategoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	cat, err := m.svc.UpdateCategory(c.Request.Context(), c.Param("slug"), input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{"category": cat})
}

func (m *Module) deleteCategory(c *gin.Context) {
	if err := m.svc.DeleteCategory(c.Request.Context(), c.Param("slug")); err != nil {
		c.Error(err)
		return
	}
	httpserver.NoContent(c)
}

// --- Tag handlers ---

func (m *Module) listTags(c *gin.Context) {
	tags, err := m.svc.ListTags(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{"tags": tags})
}

func (m *Module) createTag(c *gin.Context) {
	var input TagInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	tag, err := m.svc.CreateTag(c.Request.Context(), input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.Created(c, gin.H{"tag": tag})
}

func (m *Module) updateTag(c *gin.Context) {
	var input TagInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	tag, err := m.svc.UpdateTag(c.Request.Context(), c.Param("slug"), input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{"tag": tag})
}

func (m *Module) deleteTag(c *gin.Context) {
	if err := m.svc.DeleteTag(c.Request.Context(), c.Param("slug")); err != nil {
		c.Error(err)
		return
	}
	httpserver.NoContent(c)
}

var _ = http.StatusOK
