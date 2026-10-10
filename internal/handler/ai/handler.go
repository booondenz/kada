// Package ai serves the assistant's HTTP surface.
//
// It used to be a reverse proxy to a Python service on 127.0.0.1:8000. The assistant runs in this process
// now, so what is left of that boundary is the paths and the frames: /api/ai/chat streams server-sent
// events named token, done and error, and /api/ai/conversations/* answers JSON. Both are unchanged,
// because the frontend depends on them and not on what answers.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chun/kada-backend/internal/assistant"
	"github.com/chun/kada-backend/internal/middleware"
)

// Assistant is the slice of the assistant this handler needs.
//
// The handler declares it rather than taking the concrete type so its tests can stream a scripted answer,
// with no model, no database and no network.
type Assistant interface {
	Stream(ctx context.Context, userID int64, conversationID, question string) (<-chan assistant.Event, error)
	Current(ctx context.Context, userID int64) (assistant.Conversation, error)
	Restart(ctx context.Context, userID int64) (string, error)
}

// Handler serves /api/ai/*.
type Handler struct {
	assistant Assistant
}

// NewHandler builds the handler on an assistant (which may be wired to nothing at all - see cmd/server).
func NewHandler(a Assistant) *Handler {
	return &Handler{assistant: a}
}

// RegisterRoutes mounts the AI surface on the /api router group.
func (h *Handler) RegisterRoutes(r *gin.RouterGroup, authMW gin.HandlerFunc) {
	r.Use(authMW)
	r.POST("/ai/chat", h.chat)
	// Single-session mode: current returns the user's latest conversation with its messages, restart drops
	// it and creates a fresh empty one. The paths say conversations because the table is ai_conversations
	// and the response already carries conversation_id.
	r.GET("/ai/conversations/current", h.current)
	r.POST("/ai/conversations/restart", h.restart)
}

type chatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
}

// chat streams one answer as server-sent events.
func (h *Handler) chat(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated user required"})
		return
	}

	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
		return
	}

	// SSE is a long-lived response and cmd/server sets a 10 second write deadline, which would cut the
	// stream off mid-answer. Clearing it for this request is what lets a long reply finish.
	if ctrl := http.NewResponseController(c.Writer); ctrl != nil {
		if err := ctrl.SetWriteDeadline(time.Time{}); err != nil {
			log.Printf("ai: could not clear the write deadline: %v", err)
		}
	}

	events, err := h.assistant.Stream(c.Request.Context(), userID, req.ConversationID, req.Message)
	if err != nil {
		// Nothing has been written yet, so a failure here can still be an ordinary response; the AI page
		// shows the body as the error message. Everything after the first frame can only be an error event.
		c.String(http.StatusServiceUnavailable, "AI 服务异常：%v", err)
		return
	}

	// X-Accel-Buffering stops nginx from collecting the whole stream into one response, which would make
	// the answer appear in a lump instead of word by word.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	flusher, _ := c.Writer.(http.Flusher)
	for event := range events {
		if err := writeEvent(c.Writer, event); err != nil {
			// The client is gone; the producer stops on its own because the request context is canceled.
			log.Printf("ai: failed to write an event: %v", err)
			break
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// current returns the user's latest conversation and its messages.
func (h *Handler) current(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated user required"})
		return
	}

	conversation, err := h.assistant.Current(c.Request.Context(), userID)
	if err != nil {
		log.Printf("ai: failed to load the conversation for user %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load the conversation"})
		return
	}
	c.JSON(http.StatusOK, conversation)
}

// restart drops the user's conversations and returns a fresh one.
func (h *Handler) restart(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authenticated user required"})
		return
	}

	conversationID, err := h.assistant.Restart(c.Request.Context(), userID)
	if err != nil {
		log.Printf("ai: failed to restart the conversation for user %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to restart the conversation"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversation_id": conversationID})
}

// writeEvent writes one SSE frame: the event line, the data line and the blank line that ends it.
func writeEvent(w io.Writer, event assistant.Event) error {
	payload, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("failed to encode an event: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, payload); err != nil {
		return fmt.Errorf("failed to write an event: %w", err)
	}
	return nil
}
