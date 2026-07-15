package comments

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestListByPostRejectsOverflowingPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/", nil)

	svc := &Service{}
	_, err := svc.ListByPost(ctx, "post", math.MaxInt, 50)
	if err == nil {
		t.Fatal("ListByPost() accepted a page whose offset overflows int")
	}
}
