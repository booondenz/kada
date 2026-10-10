package assistant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain/entity"
)

// historyLimit is how many turns the model is given. It is the same 50 the Python service used
// (settings.MAX_MESSAGES): a context-window decision, not an implementation detail of the port.
const historyLimit = 50

// ChatMessage is one turn of a conversation, in the shape the model and the frontend both read.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Conversation is a session as the frontend sees it. ConversationID is null for a user who has never
// chatted, which is what the AI page renders as an empty history.
type Conversation struct {
	ConversationID *string       `json:"conversation_id"`
	Messages       []ChatMessage `json:"messages"`
}

// Conversations stores chat history in PostgreSQL.
//
// It is the only copy. The Python service kept a Redis cache in front of the same data; that is gone on
// purpose - the reads are one indexed SELECT per question, and a cache that can disagree with the
// database is one more thing to reason about for no measurable gain at this size.
type Conversations struct {
	db *gorm.DB
}

// NewConversations builds the store on an open database handle.
func NewConversations(db *gorm.DB) *Conversations {
	return &Conversations{db: db}
}

// Start creates an empty conversation for the user and returns its id.
func (c *Conversations) Start(ctx context.Context, userID int64) (string, error) {
	conversation := entity.AIConversation{ID: uuid.New(), UserID: userID}
	if err := c.db.WithContext(ctx).Create(&conversation).Error; err != nil {
		return "", fmt.Errorf("failed to start a conversation: %w", err)
	}
	return conversation.ID.String(), nil
}

// Load returns the recent turns of a conversation, oldest first.
//
// A conversation that does not exist, or belongs to somebody else, yields no turns and no error: the
// ownership condition is in the query, so the rows simply do not match, and the caller treats that as a
// new conversation. An id that is not a UUID is the same thing - it cannot be a conversation this user
// owns.
func (c *Conversations) Load(ctx context.Context, userID int64, conversationID string) ([]ChatMessage, error) {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, nil //nolint:nilerr // an unparsable id is not an error, it is not this user's conversation
	}

	var rows []entity.AIMessage
	if err := c.historyQuery(c.db.WithContext(ctx), userID, id).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load the conversation: %w", err)
	}
	return toMessages(rows), nil
}

// Append stores one turn. A conversation that does not belong to the user is ignored rather than
// reported: the only way to reach that branch is a forged or stale id from the client, and the caller has
// nothing useful to do with the error.
func (c *Conversations) Append(ctx context.Context, userID int64, conversationID, role, content string) error {
	id, err := uuid.Parse(conversationID)
	if err != nil {
		return nil //nolint:nilerr // see Load: the id is not a conversation, so there is nothing to append to
	}

	owned, err := c.owned(ctx, userID, id)
	if err != nil {
		return err
	}
	if !owned {
		return nil
	}

	message := entity.AIMessage{
		ID:             uuid.New(),
		ConversationID: id,
		Role:           role,
		Content:        content,
	}
	if err := c.db.WithContext(ctx).Create(&message).Error; err != nil {
		return fmt.Errorf("failed to store the message: %w", err)
	}
	return nil
}

// Current returns the user's latest conversation with its turns, or an empty conversation.
func (c *Conversations) Current(ctx context.Context, userID int64) (Conversation, error) {
	var latest entity.AIConversation
	err := c.latestQuery(c.db.WithContext(ctx), userID).First(&latest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Conversation{Messages: []ChatMessage{}}, nil
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("failed to load the latest conversation: %w", err)
	}

	messages, err := c.Load(ctx, userID, latest.ID.String())
	if err != nil {
		return Conversation{}, err
	}
	id := latest.ID.String()
	return Conversation{ConversationID: &id, Messages: messages}, nil
}

// Restart drops every conversation the user has and starts a new one.
//
// The messages go with them through ON DELETE CASCADE, which is declared on the model and therefore in the
// database: a delete here cannot leave turns behind.
func (c *Conversations) Restart(ctx context.Context, userID int64) (string, error) {
	err := c.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&entity.AIConversation{}).Error
	if err != nil {
		return "", fmt.Errorf("failed to drop the previous conversations: %w", err)
	}
	return c.Start(ctx, userID)
}

// owned reports whether the conversation belongs to the user.
func (c *Conversations) owned(ctx context.Context, userID int64, id uuid.UUID) (bool, error) {
	var count int64
	err := c.db.WithContext(ctx).
		Model(&entity.AIConversation{}).
		Where("id = ? AND user_id = ?", id, userID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check the conversation owner: %w", err)
	}
	return count > 0, nil
}

// historyQuery selects a conversation's turns, newest first.
//
// The join is what enforces ownership: a turn is only reached through a conversation that belongs to this
// user, and the limit therefore always trims the *old* end of that user's own history.
func (c *Conversations) historyQuery(query *gorm.DB, userID int64, id uuid.UUID) *gorm.DB {
	return query.
		Model(&entity.AIMessage{}).
		Select("ai_messages.*").
		Joins("JOIN ai_conversations ON ai_conversations.id = ai_messages.conversation_id").
		Where("ai_messages.conversation_id = ? AND ai_conversations.user_id = ?", id, userID).
		Order("ai_messages.created_at DESC").
		Limit(historyLimit)
}

// latestQuery selects the user's most recent conversation.
func (c *Conversations) latestQuery(query *gorm.DB, userID int64) *gorm.DB {
	return query.
		Model(&entity.AIConversation{}).
		Where("user_id = ?", userID).
		Order("created_at DESC")
}

// toMessages turns stored rows into turns in reading order.
//
// The query takes the newest rows (DESC) so that the limit keeps the recent end of a long conversation;
// the model has to read them oldest first, so they are reversed here.
func toMessages(rows []entity.AIMessage) []ChatMessage {
	messages := make([]ChatMessage, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		messages = append(messages, ChatMessage{Role: rows[i].Role, Content: rows[i].Content})
	}
	return messages
}
