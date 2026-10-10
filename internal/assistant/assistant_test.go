package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// fakeRound is one reply the fake model gives: either text, or a request to call tools.
type fakeRound struct {
	chunks    []string
	toolCalls []schema.ToolCall
}

// fakeModel is an Eino chat model that streams scripted replies, one per call. That interface is why the
// assistant can be tested end to end without a key, a network or a provider - and, with the real agent
// behind it, why the tool loop is exercised rather than mocked.
type fakeModel struct {
	// chunks is a single reply, for the tests that never leave the first round.
	chunks []string
	// rounds, when set, is one reply per model call: a tool request, then the answer that reads its result.
	rounds      []fakeRound
	calls       int
	generateErr error
	streamErr   error
	requests    [][]*schema.Message
	// historyWhenCalled is how many turns the store held when the model was called, which is how the test
	// sees that the question was stored first.
	historyWhenCalled int
	onCall            func()
}

func (m *fakeModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("Generate is not used by the assistant")
}

func (m *fakeModel) Stream(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.requests = append(m.requests, messages)
	if m.onCall != nil {
		m.onCall()
	}
	if m.streamErr != nil {
		return nil, m.streamErr
	}

	round := fakeRound{chunks: m.chunks}
	if len(m.rounds) > 0 {
		if m.calls >= len(m.rounds) {
			return nil, errors.New("the model was called more times than the test scripted")
		}
		round = m.rounds[m.calls]
	}
	m.calls++

	stream, writer := schema.Pipe[*schema.Message](len(round.chunks) + 1)
	go func() {
		defer writer.Close()
		if len(round.toolCalls) > 0 {
			writer.Send(schema.AssistantMessage("", round.toolCalls), nil)
			return
		}
		for _, chunk := range round.chunks {
			writer.Send(schema.AssistantMessage(chunk, nil), nil)
		}
	}()
	return stream, nil
}

func (m *fakeModel) BindTools([]*schema.ToolInfo) error { return nil }

func (m *fakeModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) { return m, nil }

// fakeHistory records the turns in the order the assistant stores them.
type fakeHistory struct {
	stored     []ChatMessage
	started    string
	appended   []ChatMessage
	current    Conversation
	loadErr    error
	appendErr  error
	currentErr error
}

func (h *fakeHistory) Start(context.Context, int64) (string, error) {
	h.started = "new-conversation"
	return h.started, nil
}

func (h *fakeHistory) Current(context.Context, int64) (Conversation, error) {
	return h.current, h.currentErr
}

func (h *fakeHistory) Restart(context.Context, int64) (string, error) {
	h.appended = nil
	return "restarted-conversation", nil
}

func (h *fakeHistory) Load(context.Context, int64, string) ([]ChatMessage, error) {
	if h.loadErr != nil {
		return nil, h.loadErr
	}
	return append([]ChatMessage(nil), h.stored...), nil
}

func (h *fakeHistory) Append(_ context.Context, _ int64, _ string, role, content string) error {
	if h.appendErr != nil {
		return h.appendErr
	}
	h.appended = append(h.appended, ChatMessage{Role: role, Content: content})
	return nil
}

// newTestAssistant builds the assistant the way the process does - the real agent with the real tools - and
// fakes only the model and the store. That is what makes the tool test below a test of the wiring rather
// than of a stand-in.
func newTestAssistant(t *testing.T, chatModel model.ToolCallingChatModel, convos history) *Assistant {
	t.Helper()
	a, err := NewAssistant(context.Background(), chatModel, &fakeKada{}, convos)
	if err != nil {
		t.Fatalf("NewAssistant() failed: %v", err)
	}
	return a
}

// collect drains the events of one turn.
func collect(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var out []Event
	for event := range events {
		out = append(out, event)
	}
	return out
}

func deltas(events []Event) []string {
	var out []string
	for _, event := range events {
		if event.Name == EventToken {
			out = append(out, event.Data.(tokenPayload).Delta)
		}
	}
	return out
}

// The answer arrives token by token and the last event is the done event, which is what lets the page
// remember the conversation for the next question.
func TestStreamEmitsTokensThenDone(t *testing.T) {
	history := &fakeHistory{}
	a := newTestAssistant(t, &fakeModel{chunks: []string{"你", "好"}}, history)

	events, err := a.Stream(context.Background(), 7, "c-1", "打个招呼")
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}
	got := collect(t, events)

	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[0].Name != EventToken || got[1].Name != EventToken || got[2].Name != EventDone {
		t.Fatalf("unexpected event order: %+v", got)
	}
	if after := deltas(got); after[0] != "你" || after[1] != "好" {
		t.Errorf("deltas = %v, want [你 好]", after)
	}
	if payload := got[2].Data.(donePayload); payload.ConversationID != "c-1" {
		t.Errorf("done carries conversation %q, want c-1", payload.ConversationID)
	}

	// Both turns are stored: the question before the model runs, the answer after it finishes.
	if len(history.appended) != 2 ||
		history.appended[0].Role != "user" || history.appended[0].Content != "打个招呼" ||
		history.appended[1].Role != "assistant" || history.appended[1].Content != "你好" {
		t.Errorf("stored turns = %+v, want the question then the answer", history.appended)
	}
}

// The question is stored before the model is called: an answer that fails half way must still leave the
// user's question behind, which is what makes a retry readable.
func TestStreamStoresTheQuestionBeforeTheModelRuns(t *testing.T) {
	history := &fakeHistory{}
	chatModel := &fakeModel{chunks: []string{"好"}}
	chatModel.onCall = func() { chatModel.historyWhenCalled = len(history.appended) }
	a := newTestAssistant(t, chatModel, history)

	events, err := a.Stream(context.Background(), 7, "c-1", "问题")
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}
	collect(t, events)

	if chatModel.historyWhenCalled != 1 {
		t.Errorf("the store held %d turns when the model was called, want the question already stored",
			chatModel.historyWhenCalled)
	}
}

// A conversation id the caller does not have yet means a new conversation, and the done event is what
// tells the page which one it got.
func TestStreamStartsAConversationWhenThereIsNone(t *testing.T) {
	history := &fakeHistory{}
	a := newTestAssistant(t, &fakeModel{chunks: []string{"好"}}, history)

	events, err := a.Stream(context.Background(), 7, "", "第一句")
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}
	got := collect(t, events)

	if history.started != "new-conversation" {
		t.Error("no conversation was started for a request without one")
	}
	if payload := got[len(got)-1].Data.(donePayload); payload.ConversationID != "new-conversation" {
		t.Errorf("done carries conversation %q, want the new one", payload.ConversationID)
	}
}

// The documentation reaches the model on the way in, which is what turns the assistant from a general chat
// model into one that knows this product.
func TestStreamGivesTheModelTheDocumentation(t *testing.T) {
	chatModel := &fakeModel{chunks: []string{"好"}}
	a := newTestAssistant(t, chatModel, &fakeHistory{})

	events, err := a.Stream(context.Background(), 7, "c-1", "短链是什么")
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}
	collect(t, events)

	first := chatModel.requests[0][0]
	if first.Role != schema.System {
		t.Fatalf("first message role = %q, want the system prompt", first.Role)
	}
	if !strings.Contains(first.Content, "【参考资料】") {
		t.Errorf("system message = %q, want the reference block", first.Content)
	}
	last := chatModel.requests[0][len(chatModel.requests[0])-1]
	if last.Role != schema.User || last.Content != "短链是什么" {
		t.Errorf("last message = %+v, want the question", last)
	}
}

// A model that refuses the request is reported as an error event - the headers are already sent, so there
// is no status code left to use - and nothing is stored as the answer.
func TestStreamReportsAModelFailureAsAnErrorEvent(t *testing.T) {
	history := &fakeHistory{}
	a := newTestAssistant(t, &fakeModel{streamErr: errors.New("401 unauthorized")}, history)

	events, err := a.Stream(context.Background(), 7, "c-1", "问题")
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}
	got := collect(t, events)

	if len(got) != 1 || got[0].Name != EventError {
		t.Fatalf("events = %+v, want a single error event", got)
	}
	if message := got[0].Data.(errorPayload).Message; !strings.HasPrefix(message, "AI 服务异常：") {
		t.Errorf("message = %q, want the page's prefix", message)
	}
	for _, turn := range history.appended {
		if turn.Role == "assistant" {
			t.Error("a failed answer was stored")
		}
	}
}

// A question that cannot even be loaded fails before the stream starts, so the handler can still answer
// with a status code. This is the branch the page turns into a toast rather than an empty bubble.
func TestStreamFailsBeforeTheFirstEvent(t *testing.T) {
	a := newTestAssistant(t, &fakeModel{}, &fakeHistory{loadErr: errors.New("database is down")})

	if _, err := a.Stream(context.Background(), 7, "c-1", "问题"); err == nil {
		t.Fatal("expected the load failure to be returned to the caller")
	}
}

// When the browser goes away the producer stops: no done event, no half-written answer stored. Without the
// context check the goroutine would block forever on a channel nobody reads.
func TestStreamStopsWhenTheClientGoesAway(t *testing.T) {
	history := &fakeHistory{}
	a := newTestAssistant(t, &fakeModel{chunks: []string{"你", "好"}}, history)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	events, err := a.Stream(ctx, 7, "c-1", "问题")
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}
	// collect returning at all is the first assertion: the producer stopped instead of blocking forever on a
	// channel nobody reads. Which events arrive depends on the reader - a canceled context makes the agent
	// fail, and whether that failure is deliverable is up to whoever is still listening.
	got := collect(t, events)

	if len(got) > 0 && got[len(got)-1].Name == EventDone {
		t.Errorf("a canceled request reported a finished answer: %+v", got)
	}
	for _, turn := range history.appended {
		if turn.Role == "assistant" {
			t.Error("an answer nobody could read was stored")
		}
	}
}

// The agent is what runs the tools: the model asks for one, the agent executes it and calls the model again
// with the result. That is the loop the Python service wrote by hand, and the user in the context is what
// makes one shared agent safe to reuse for every account.
func TestStreamRunsTheToolTheModelAsksFor(t *testing.T) {
	kada := &fakeKada{overview: `{"total_links":3,"total_clicks":9}`}
	chatModel := &fakeModel{rounds: []fakeRound{
		{toolCalls: []schema.ToolCall{{
			ID:       "call-1",
			Function: schema.FunctionCall{Name: "get_link_overview", Arguments: "{}"},
		}}},
		{chunks: []string{"你有 ", "3 条短链"}},
	}}
	history := &fakeHistory{}

	a, err := NewAssistant(context.Background(), chatModel, kada, history)
	if err != nil {
		t.Fatalf("NewAssistant() failed: %v", err)
	}
	events, err := a.Stream(context.Background(), 42, "c-1", "我一共有多少短链")
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}
	got := collect(t, events)

	if kada.overviewCalls != 1 {
		t.Errorf("the tool ran %d times, want once", kada.overviewCalls)
	}
	if kada.userID != 42 {
		t.Errorf("the tool acted for user %d, want the caller 42", kada.userID)
	}
	if after := deltas(got); len(after) != 2 || after[0] != "你有 " {
		t.Errorf("deltas = %v, want the answer from the round after the tool", after)
	}
	if got[len(got)-1].Name != EventDone {
		t.Errorf("the turn did not finish: %+v", got)
	}

	// The model's second request carries the tool's result, which is how it learned the numbers.
	second := chatModel.requests[1]
	last := second[len(second)-1]
	if last.Role != schema.Tool || !strings.Contains(last.Content, "total_links") {
		t.Errorf("the second request did not carry the tool result: %+v", last)
	}
}

// A model that only ever asks for tools has to hit the step limit. Without it this test would never return,
// which is exactly what the bound is for.
func TestStreamStopsAtTheStepLimit(t *testing.T) {
	rounds := make([]fakeRound, 0, maxAgentSteps+2)
	for i := 0; i < maxAgentSteps+2; i++ {
		rounds = append(rounds, fakeRound{toolCalls: []schema.ToolCall{{
			ID:       "call-1",
			Function: schema.FunctionCall{Name: "get_current_time", Arguments: "{}"},
		}}})
	}

	a := newTestAssistant(t, &fakeModel{rounds: rounds}, &fakeHistory{})
	events, err := a.Stream(context.Background(), 7, "c-1", "不停地调用工具")
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}
	got := collect(t, events)

	if len(got) == 0 {
		t.Fatal("the turn produced no events at all")
	}
	// However it ends, it ends: a token as the last event would mean the stream was cut without saying so.
	if last := got[len(got)-1].Name; last != EventDone && last != EventError {
		t.Errorf("the turn ended on a %s event, want done or error: %+v", last, got)
	}
}
