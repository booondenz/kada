package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/chun/kada-backend/internal/assistant"
)

// fakeAuth mimics the real JWT middleware: it stamps the authenticated user id into the context. The AI
// handler reads it from there and never from the request, which is what these tests check.
func fakeAuth(userID int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	}
}

// fakeAssistant streams a scripted answer and records what it was asked.
type fakeAssistant struct {
	events         []assistant.Event
	streamErr      error
	conversation   assistant.Conversation
	currentErr     error
	restarted      string
	restartErr     error
	userID         int64
	conversationID string
	question       string
}

func (f *fakeAssistant) Stream(_ context.Context, userID int64, conversationID, question string) (<-chan assistant.Event, error) {
	f.userID, f.conversationID, f.question = userID, conversationID, question
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	events := make(chan assistant.Event, len(f.events))
	for _, event := range f.events {
		events <- event
	}
	close(events)
	return events, nil
}

func (f *fakeAssistant) Current(context.Context, int64) (assistant.Conversation, error) {
	return f.conversation, f.currentErr
}

func (f *fakeAssistant) Restart(context.Context, int64) (string, error) {
	return f.restarted, f.restartErr
}

func newTestRouter(userID int64, a Assistant) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(a).RegisterRoutes(r.Group("/api"), fakeAuth(userID))
	return r
}

func postJSON(r *gin.Engine, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func get(r *gin.Engine, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

// The frames, in this shape, are what the AI page parses: an event line, a data line, a blank line. The
// frontend was not touched by moving the assistant into this process, so a change here is a change there.
func TestChatStreamsTheFramesThePageParses(t *testing.T) {
	fake := &fakeAssistant{events: []assistant.Event{
		{Name: assistant.EventToken, Data: struct {
			Delta string `json:"delta"`
		}{Delta: "你好"}},
		{Name: assistant.EventToken, Data: struct {
			Delta string `json:"delta"`
		}{Delta: "，世界"}},
		{Name: assistant.EventDone, Data: struct {
			ConversationID string `json:"conversation_id"`
		}{ConversationID: "conv-1"}},
	}}
	r := newTestRouter(42, fake)

	w := postJSON(r, "/api/ai/chat", `{"conversation_id":"conv-1","message":"你好"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	// nginx buffers by default, which turns a stream into one lump at the end.
	if got := w.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
	want := "event: token\ndata: {\"delta\":\"你好\"}\n\n" +
		"event: token\ndata: {\"delta\":\"，世界\"}\n\n" +
		"event: done\ndata: {\"conversation_id\":\"conv-1\"}\n\n"
	if w.Body.String() != want {
		t.Errorf("body =\n%q\nwant\n%q", w.Body.String(), want)
	}
}

// The user id is the authenticated one. There is no header to spoof any more - the proxy that used to
// inject one is gone - so this asserts the value comes from the middleware and nothing else.
func TestChatUsesTheAuthenticatedUser(t *testing.T) {
	fake := &fakeAssistant{}
	r := newTestRouter(42, fake)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/chat", strings.NewReader(`{"message":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kada-User-ID", "999")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if fake.userID != 42 {
		t.Errorf("assistant saw user %d, want the authenticated 42", fake.userID)
	}
	if fake.question != "hi" {
		t.Errorf("assistant saw question %q, want %q", fake.question, "hi")
	}
}

// An unauthenticated request must not reach the assistant at all.
func TestChatRequiresAnAuthenticatedUser(t *testing.T) {
	fake := &fakeAssistant{}
	r := newTestRouter(0, fake)

	w := postJSON(r, "/api/ai/chat", `{"message":"hi"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if fake.question != "" {
		t.Error("the assistant was called without an authenticated user")
	}
}

func TestChatRejectsAnEmptyMessage(t *testing.T) {
	r := newTestRouter(42, &fakeAssistant{})

	w := postJSON(r, "/api/ai/chat", `{"message":"   "}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// A failure before the first frame can still be an HTTP error, and the page shows the body as the message.
func TestChatReportsAnEarlyFailureAs503(t *testing.T) {
	fake := &fakeAssistant{streamErr: errors.New("the chat model API key is empty")}
	r := newTestRouter(42, fake)

	w := postJSON(r, "/api/ai/chat", `{"message":"hi"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "AI 服务异常：") {
		t.Errorf("body = %q, want the assistant's error message", w.Body.String())
	}
}

// A failure after the headers are on the wire cannot be an HTTP status: it has to be an error event in the
// stream, which is what the page's `event: error` branch exists for.
func TestChatStreamsAFailureAsAnErrorEvent(t *testing.T) {
	fake := &fakeAssistant{events: []assistant.Event{
		{Name: assistant.EventToken, Data: struct {
			Delta string `json:"delta"`
		}{Delta: "半"}},
		{Name: assistant.EventError, Data: struct {
			Message string `json:"message"`
		}{Message: "AI 服务异常：boom"}},
	}}
	r := newTestRouter(42, fake)

	w := postJSON(r, "/api/ai/chat", `{"message":"hi"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: error\ndata: {\"message\":\"AI 服务异常：boom\"}\n\n") {
		t.Errorf("body = %q, want an error frame", body)
	}
}

// A user who has never chatted gets a null conversation id and no messages: the page renders that as an
// empty history rather than as an error.
func TestCurrentReturnsAnEmptyConversation(t *testing.T) {
	r := newTestRouter(42, &fakeAssistant{conversation: assistant.Conversation{Messages: []assistant.ChatMessage{}}})

	w := get(r, "/api/ai/conversations/current")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != `{"conversation_id":null,"messages":[]}` {
		t.Errorf("body = %q", got)
	}
}

func TestCurrentReturnsTheStoredTurns(t *testing.T) {
	r := newTestRouter(42, &fakeAssistant{conversation: assistant.Conversation{
		ConversationID: ptr("c-1"),
		Messages: []assistant.ChatMessage{
			{Role: "user", Content: "你好"},
			{Role: "assistant", Content: "你好，有什么可以帮你？"},
		},
	}})

	w := get(r, "/api/ai/conversations/current")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	want := `{"conversation_id":"c-1","messages":[{"role":"user","content":"你好"},` +
		`{"role":"assistant","content":"你好，有什么可以帮你？"}]}`
	if got := w.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestRestartReturnsTheNewConversation(t *testing.T) {
	r := newTestRouter(42, &fakeAssistant{restarted: "c-2"})

	w := postJSON(r, "/api/ai/conversations/restart", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != `{"conversation_id":"c-2"}` {
		t.Errorf("body = %q", got)
	}
}

func ptr(value string) *string { return &value }
