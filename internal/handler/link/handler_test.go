package link

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
)

func testContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}

// A ceiling that has been reached is a 429 with a retry-after. Answering 500 tells the client to retry the one
// request that cannot succeed until the window moves.
func TestRespondLinkError_ReportsACeilingAsTooManyRequests(t *testing.T) {
	c, w := testContext(t)

	respondLinkError(c, &domain.RateLimitError{
		Message:           "this account can create 20 links a day, try again later",
		RetryAfterSeconds: 90,
	}, http.StatusInternalServerError)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if got := w.Header().Get("Retry-After"); got != "90" {
		t.Errorf("Retry-After = %q, want 90", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "20 links a day") || !strings.Contains(body, "90") {
		t.Errorf("body = %s, want the message and the wait", body)
	}
}

// A target in a blocked category is the caller's fault, on both the create and the edit path.
func TestRespondLinkError_ReportsABlockedTargetAsABadRequest(t *testing.T) {
	c, w := testContext(t)

	respondLinkError(c, &domain.BlockedTargetError{Category: "gambling"}, http.StatusNotFound)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if body := w.Body.String(); !strings.Contains(body, "gambling") {
		t.Errorf("body = %s, want the category named", body)
	}
}

// Everything else keeps the status the path chose: a failed create is a server fault, and a failed edit is
// usually a link that is not there.
func TestRespondLinkError_KeepsThePathFallback(t *testing.T) {
	for _, fallback := range []int{http.StatusInternalServerError, http.StatusNotFound} {
		c, w := testContext(t)

		respondLinkError(c, errors.New("failed to update link, link not found or access denied"), fallback)

		if w.Code != fallback {
			t.Errorf("status = %d, want %d", w.Code, fallback)
		}
		if got := w.Header().Get("Retry-After"); got != "" {
			t.Errorf("an ordinary failure must not ask for a retry-after, got %q", got)
		}
	}
}
