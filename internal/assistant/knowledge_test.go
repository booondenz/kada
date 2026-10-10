package assistant

import (
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// The documentation is compiled into the binary, so a build that shipped without it would answer every
// product question from guesswork and nothing else would fail. This is the assertion that catches it.
func TestReferenceHoldsTheBundledDocumentation(t *testing.T) {
	docs, err := readKnowledgeFiles()
	if err != nil {
		t.Fatalf("readKnowledgeFiles() failed: %v", err)
	}
	if strings.TrimSpace(docs) == "" {
		t.Fatal("the bundled documentation is empty")
	}

	// Every file under knowledge/ has to be in the block: a second document the glob misses would be a
	// document nobody reads, which is worse than not writing it.
	paths, err := fs.Glob(knowledgeFiles, "knowledge/*.md")
	if err != nil {
		t.Fatalf("listing the bundled documentation failed: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no documentation is embedded at all")
	}
	for _, path := range paths {
		text, err := knowledgeFiles.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s failed: %v", path, err)
		}
		if !strings.Contains(docs, strings.TrimSpace(string(text))) {
			t.Errorf("%s is missing from the reference block", path)
		}
	}
}

// The model has to be told what Kada is before it is asked about Kada: the documentation travels in the
// system message, and the question stays a user message of its own.
func TestBuildMessagesCarriesTheDocumentationAndTheQuestion(t *testing.T) {
	messages := buildMessages([]ChatMessage{{Role: "user", Content: "先前的提问"}}, "怎么创建短链接")

	if len(messages) != 3 {
		t.Fatalf("got %d messages, want the system message, the history and the question", len(messages))
	}
	if messages[0].Role != schema.System {
		t.Errorf("first message role = %q, want the system prompt", messages[0].Role)
	}
	if !strings.HasPrefix(messages[0].Content, systemPrompt) {
		t.Errorf("system message starts with %q, want the prompt verbatim", messages[0].Content)
	}

	docs, err := reference()
	if err != nil {
		t.Fatalf("reference() failed: %v", err)
	}
	if !strings.Contains(messages[0].Content, docs) {
		t.Error("the system message does not carry the bundled documentation")
	}

	if last := messages[len(messages)-1]; last.Role != schema.User || last.Content != "怎么创建短链接" {
		t.Errorf("last message = %+v, want the question and nothing else", last)
	}
}

// Documentation that cannot be read is not a reason to refuse to chat: the assistant answers from the model
// alone, with the prompt it would otherwise have appended the reference to.
func TestTheAssistantAnswersWhenTheDocumentationCannotBeRead(t *testing.T) {
	original := reference
	reference = sync.OnceValues(func() (string, error) {
		return "", errors.New("the bundled documentation is empty")
	})
	defer func() { reference = original }()

	if got := systemMessage(); got != systemPrompt {
		t.Errorf("system message = %q, want the prompt alone", got)
	}
}
