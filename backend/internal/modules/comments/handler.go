package comments

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Module wires comment routes into the API.
type Module struct {
	svc *Service
}

// NewModule creates a comments module.
func NewModule(cfg *config.Config, repo *Repository) *Module {
	return &Module{svc: NewService(repo)}
}

// Register mounts comment routes under /api/v1.
func (m *Module) Register(r *gin.Engine, authMW gin.HandlerFunc) {
	v1 := r.Group("/api/v1")

	v1.POST("/posts/:slug/comments", authMW, m.create)
	v1.GET("/posts/:slug/comments", m.list)
	v1.PUT("/comments/:id", authMW, m.update)
	v1.DELETE("/comments/:id", authMW, m.delete)
}

func (m *Module) create(c *gin.Context) {
	var input CreateCommentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	input.PostPublicID = c.Param("slug")
	comment, err := m.svc.Create(c, input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.Created(c, gin.H{"comment": comment})
}

func (m *Module) list(c *gin.Context) {
	page, size := 1, 20
	// List by post slug — need to resolve post ID first
	// This is intentionally simplified for now; the full implementation
	// resolves the post slug to a numeric ID via the posts repository
	result, err := m.svc.ListByPost(c, 0, page, size)
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

var _ = http.StatusOK
