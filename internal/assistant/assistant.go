// Package assistant is the assistant: the documentation it answers from, the model it answers with and the
// tools it may call.
//
// It runs inside the API process and replaces a separate Python service (backend/ai) that the API used to
// reverse-proxy to on 127.0.0.1:8000. The model client comes from Eino (github.com/cloudwego/eino); the
// conversations are PostgreSQL, in the same database as everything else.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

// The event names and payloads the AI page reads. They are the Python service's, unchanged: the frontend
// switches on the name and reads the JSON, so a rename here is a frontend change.
const (
	EventToken = "token"
	EventDone  = "done"
	EventError = "error"
)

// Event is one server-sent event. The handler turns it into a frame; producing events rather than writing
// SSE keeps the transport out of this package.
type Event struct {
	Name string
	Data any
}

type tokenPayload struct {
	Delta string `json:"delta"`
}

type usagePayload struct {
	Tokens int `json:"tokens"`
}

type donePayload struct {
	ConversationID string       `json:"conversation_id"`
	Usage          usagePayload `json:"usage"`
}

type errorPayload struct {
	Message string `json:"message"`
}

// systemPrompt is the Python service's prompt, verbatim. It is the assistant's behavior rather than an
// implementation detail: rewriting it while porting would change how it answers, which is not what a port
// is for.
const systemPrompt = "你是 kada 平台的 AI 助手，帮助用户分析和管理短链接数据。请用简体中文回答，" +
	"简洁准确。优先使用参考资料；需要实时信息或计算时，可以调用提供的工具。"

// systemMessage is the instructions the model is given: the prompt above and then the whole bundled
// documentation.
//
// The documentation travels in the prompt rather than behind a search. The corpus is a few kilobytes of
// product notes, and at that size retrieval can only lose a passage the model needed, while it costs a
// second vendor key, a PostgreSQL with the vector extension and an indexing step in the deploy. The comment
// on reference names the seam to put retrieval back behind if the corpus ever outgrows a prompt.
func systemMessage() string {
	docs, err := reference()
	if err != nil {
		// The assistant stays usable: it answers from the model alone, exactly what the Python service did
		// when its knowledge base was unreachable. A product question then becomes a guess, which is a worse
		// answer rather than no answer - and not worth failing a chat over.
		log.Printf("assistant: the bundled documentation could not be read, answering without it: %v", err)
		return systemPrompt
	}
	return systemPrompt + "\n\n【参考资料】\n" + docs
}

// history is the slice of the conversation store the assistant needs.
type history interface {
	Load(ctx context.Context, userID int64, conversationID string) ([]ChatMessage, error)
	Append(ctx context.Context, userID int64, conversationID, role, content string) error
	Start(ctx context.Context, userID int64) (string, error)
	Current(ctx context.Context, userID int64) (Conversation, error)
	Restart(ctx context.Context, userID int64) (string, error)
}

// Assistant answers questions: it keeps the conversation and streams the answer back.
type Assistant struct {
	agent  *react.Agent
	convos history
}

// maxAgentSteps bounds one question's model/tool rounds.
//
// The unit is Eino's, not the Python service's: MaxStep counts graph steps, and the graph has a model node
// and a tools node, so a round costs about two. Twelve therefore allows roughly the five rounds
// MAX_AGENT_ROUNDS allowed, which exists for the same reason - a model that keeps calling tools has to
// reach a limit instead of holding the request open forever.
const maxAgentSteps = 12

// NewAssistant builds the assistant and the tool-calling agent behind it.
//
// A nil chat model is a configuration, not a programming error: cmd/server starts without a key, warns, and
// every question is answered with that fact (see Stream).
func NewAssistant(
	ctx context.Context,
	chatModel model.ToolCallingChatModel,
	kada kadaOperations,
	convos history,
) (*Assistant, error) {
	assistant := &Assistant{convos: convos}
	if chatModel == nil {
		return assistant, nil
	}

	tools, err := newTools(kada)
	if err != nil {
		return nil, fmt.Errorf("failed to build the assistant's tools: %w", err)
	}

	// The agent runs the tool loop the Python service wrote by hand: stream the model's answer, run whatever
	// tools it asked for, feed the results back, and stop after maxAgentSteps rounds.
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig:      compose.ToolsNodeConfig{Tools: tools},
		MaxStep:          maxAgentSteps,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build the agent: %w", err)
	}

	assistant.agent = agent
	return assistant, nil
}

// Stream starts one turn and returns its events as they happen.
//
// The error return is for the work that happens before the first token - starting or loading the
// conversation and storing the question - where the handler can still answer with a status code. Once the
// channel is open, a failure can only be reported as an error event: the 200 and the SSE headers are
// already on the wire. That split is the Python service's, and the AI page depends on both halves of it.
func (a *Assistant) Stream(ctx context.Context, userID int64, conversationID, question string) (<-chan Event, error) {
	// A process started without a chat model key still serves this route; it has to fail here rather than
	// on a nil interface, and before anything is stored, so a misconfigured deployment leaves no trace in
	// the conversation history.
	if a.agent == nil {
		return nil, errors.New("the assistant has no chat model configured")
	}

	if conversationID == "" {
		id, err := a.convos.Start(ctx, userID)
		if err != nil {
			return nil, err
		}
		conversationID = id
	}

	history, err := a.convos.Load(ctx, userID, conversationID)
	if err != nil {
		return nil, err
	}

	// The question is stored before the model runs, so an answer that fails half way still leaves what
	// the user asked behind.
	if err := a.convos.Append(ctx, userID, conversationID, "user", question); err != nil {
		return nil, err
	}

	// The tools act for this user; the context is how they find out which one (see userIDKey).
	ctx = withUser(ctx, userID)

	events := make(chan Event)
	go func() {
		defer close(events)
		a.run(ctx, userID, conversationID, history, question, events)
	}()
	return events, nil
}

// Current returns the user's latest conversation with its turns.
func (a *Assistant) Current(ctx context.Context, userID int64) (Conversation, error) {
	return a.convos.Current(ctx, userID)
}

// Restart drops the user's conversations and starts an empty one.
func (a *Assistant) Restart(ctx context.Context, userID int64) (string, error) {
	return a.convos.Restart(ctx, userID)
}

// run produces the events of one turn and is done when the channel is closed.
//
// The sends are guarded by the request context: when the browser goes away the handler stops reading, and
// a producer that kept sending would block on a channel nobody drains. That context is also why a
// disconnected client leaves no half-written answer behind - the store call after it fails, which is the
// same thing the Python service did when its generator was closed.
func (a *Assistant) run(
	ctx context.Context,
	userID int64,
	conversationID string,
	history []ChatMessage,
	question string,
	events chan<- Event,
) {
	send := func(event Event) bool {
		select {
		case events <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}

	messages := buildMessages(history, question)

	answer, err := a.answer(ctx, messages, send)
	if err != nil {
		send(Event{Name: EventError, Data: errorPayload{Message: "AI 服务异常：" + err.Error()}})
		return
	}
	if ctx.Err() != nil {
		return
	}

	if answer != "" {
		if err := a.convos.Append(ctx, userID, conversationID, "assistant", answer); err != nil {
			log.Printf("assistant: failed to store the answer for conversation %s: %v", conversationID, err)
		}
	}

	send(Event{Name: EventDone, Data: donePayload{
		ConversationID: conversationID,
		// The Python service reported the answer's length as "tokens". The field is part of the contract
		// and nothing displays it, so it keeps its meaning rather than its name changing under the
		// frontend's feet.
		Usage: usagePayload{Tokens: len([]rune(answer))},
	}})
}

// answer streams the agent's reply, forwarding each chunk, and returns the whole text.
//
// Only text reaches the user: the agent's tool calls and their results are its own business, which is what
// the Python service's hand-written loop did too - it forwarded chunk.content and kept the tool messages to
// itself.
func (a *Assistant) answer(ctx context.Context, messages []*schema.Message, send func(Event) bool) (string, error) {
	stream, err := a.agent.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("the model refused the request: %w", err)
	}
	defer stream.Close()

	var full strings.Builder
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return full.String(), fmt.Errorf("the model's stream failed: %w", err)
		}
		if chunk.Content == "" {
			continue
		}

		full.WriteString(chunk.Content)
		if !send(Event{Name: EventToken, Data: tokenPayload{Delta: chunk.Content}}) {
			return full.String(), nil
		}
	}
	return full.String(), nil
}

// buildMessages assembles one request: the instructions with the documentation, the conversation so far and
// the question.
//
// History stays separate messages so the model can tell who said what. The documentation belongs to the
// instructions rather than to the question, because it is the same for every turn.
func buildMessages(history []ChatMessage, question string) []*schema.Message {
	messages := make([]*schema.Message, 0, len(history)+2)
	messages = append(messages, schema.SystemMessage(systemMessage()))

	for _, turn := range history {
		if turn.Role == "assistant" {
			messages = append(messages, schema.AssistantMessage(turn.Content, nil))
			continue
		}
		messages = append(messages, schema.UserMessage(turn.Content))
	}

	messages = append(messages, schema.UserMessage(question))
	return messages
}
