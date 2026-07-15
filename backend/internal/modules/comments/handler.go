package comments

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/modules/posts"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/shared/apperr"
)

type Module struct{ svc *Service }

func NewModule(repo *Repository, postsRepo *posts.Repository) *Module {
	return &Module{svc: NewService(repo, postsRepo)}
}

func (m *Module) Register(r *gin.Engine, authMW, commentRateLimit gin.HandlerFunc) {
	v1 := r.Group("/api/v1")
	v1.POST("/posts/:slug/comments", authMW, commentRateLimit, m.create)
	v1.GET("/posts/:slug/comments", m.list)
	v1.PUT("/comments/:id", authMW, commentRateLimit, m.update)
	v1.DELETE("/comments/:id", authMW, m.delete)
}

func (m *Module) create(c *gin.Context) {
	var input CreateCommentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	comment, err := m.svc.Create(c, c.Param("slug"), input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.Created(c, gin.H{"comment": comment})
}

func (m *Module) list(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := m.svc.ListByPost(c, c.Param("slug"), page, pageSize)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, result)
}

func (m *Module) update(c *gin.Context) {
	var body struct {
		BodyMarkdown string `json:"body_markdown"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	comment, err := m.svc.Update(c, c.Param("id"), body.BodyMarkdown)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{"comment": comment})
}

func (m *Module) delete(c *gin.Context) {
	if err := m.svc.Delete(c, c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	httpserver.NoContent(c)
}
