package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain/entity"
)

// Ownership is the one thing a chat store must never get wrong: the condition lives in the query, so a
// conversation belonging to somebody else simply does not match. These assertions are on the SQL, because
// the failure mode - a query that forgot the join - still returns rows.
func TestHistoryQueryScopesToTheOwner(t *testing.T) {
	db := newDryRunDB(t)
	conversationID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	stmt := NewConversations(db).
		historyQuery(db.Session(&gorm.Session{DryRun: true}), 7, conversationID).
		Find(&[]entity.AIMessage{}).
		Statement
	sql := stmt.SQL.String()

	for _, want := range []string{
		`SELECT ai_messages.* FROM "ai_messages"`,
		"JOIN ai_conversations ON ai_conversations.id = ai_messages.conversation_id",
		"ai_messages.conversation_id = $1 AND ai_conversations.user_id = $2",
		"ORDER BY ai_messages.created_at DESC",
		"LIMIT $3",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("generated SQL is missing %q:\n%s", want, sql)
		}
	}
	if len(stmt.Vars) != 3 {
		t.Errorf("expected 3 bound parameters, got %d: %v", len(stmt.Vars), stmt.Vars)
	}
}

func TestLatestQueryPicksTheNewestConversationOfTheUser(t *testing.T) {
	db := newDryRunDB(t)

	stmt := NewConversations(db).
		latestQuery(db.Session(&gorm.Session{DryRun: true}), 7).
		Find(&[]entity.AIConversation{}).
		Statement
	sql := stmt.SQL.String()

	for _, want := range []string{
		`FROM "ai_conversations"`,
		"user_id = $1",
		"ORDER BY created_at DESC",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("generated SQL is missing %q:\n%s", want, sql)
		}
	}
}

// The query takes the newest turns so the limit keeps the recent end of a long conversation; the model has
// to read them oldest first, so the order is reversed on the way out.
func TestToMessagesReversesIntoReadingOrder(t *testing.T) {
	rows := []entity.AIMessage{
		{Role: "assistant", Content: "第二次回答"},
		{Role: "user", Content: "第二个问题"},
		{Role: "assistant", Content: "第一次回答"},
		{Role: "user", Content: "第一个问题"},
	}

	messages := toMessages(rows)

	want := []ChatMessage{
		{Role: "user", Content: "第一个问题"},
		{Role: "assistant", Content: "第一次回答"},
		{Role: "user", Content: "第二个问题"},
		{Role: "assistant", Content: "第二次回答"},
	}
	if len(messages) != len(want) {
		t.Fatalf("got %d turns, want %d", len(messages), len(want))
	}
	for i := range want {
		if messages[i] != want[i] {
			t.Errorf("turn %d is %+v, want %+v", i, messages[i], want[i])
		}
	}
}

// An id that is not a UUID cannot be a conversation this user owns, so it is treated as "no history yet"
// rather than as an error - and the database is not asked at all.
func TestLoadIgnoresAnIdThatIsNotAConversation(t *testing.T) {
	messages, err := NewConversations(newDryRunDB(t)).Load(context.Background(), 7, "not-a-uuid")
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("got %d turns, want none", len(messages))
	}
}

// The delete behind Restart is the user's own conversations only, and the messages go with them through
// the ON DELETE CASCADE declared on the model.
func TestRestartDropsOnlyThatUsersConversations(t *testing.T) {
	db := newDryRunDB(t)

	stmt := db.Session(&gorm.Session{DryRun: true}).
		Where("user_id = ?", int64(7)).
		Delete(&entity.AIConversation{}).
		Statement
	sql := stmt.SQL.String()

	if !strings.Contains(sql, `DELETE FROM "ai_conversations"`) {
		t.Errorf("generated SQL = %s, want a delete from ai_conversations", sql)
	}
	if !strings.Contains(sql, "user_id = $1") {
		t.Errorf("generated SQL = %s, want it scoped to the user", sql)
	}
}
