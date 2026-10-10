package assistant

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

// knowledgeFiles is the documentation the assistant answers product questions from, compiled into the
// binary.
//
// Embedded rather than read from disk: the deployment ships a binary to a host that has no checkout, so a
// path here would be a file that exists only on the machine that built it.
//
//go:embed knowledge/*.md
var knowledgeFiles embed.FS

// reference returns the bundled documentation, read once per process.
//
// Everything under knowledge/ becomes one block of reference material in the system prompt, which is the
// whole retrieval strategy: the corpus is a few kilobytes of product notes, small enough to send in full
// and therefore small enough that a search could only add ways to answer from less. The deploy agrees -
// there is no indexing step, no embedding key and no vector extension to install.
//
// The prompt is also where the cost lives: every question pays for the whole corpus, which is nothing at
// this size and would not be at a large one. If the corpus ever outgrows what a prompt can carry, this is
// the function to put retrieval behind; systemMessage, its only caller, does not have to change.
//
// It is a variable wrapping sync.OnceValues rather than a plain function so a test can replace the reader
// and see what the assistant does when the documentation cannot be read at all.
var reference = sync.OnceValues(readKnowledgeFiles)

// readKnowledgeFiles reads every bundled document and joins them into one block.
func readKnowledgeFiles() (string, error) {
	paths, err := fs.Glob(knowledgeFiles, "knowledge/*.md")
	if err != nil {
		return "", fmt.Errorf("failed to list the bundled documentation: %w", err)
	}
	if len(paths) == 0 {
		// A build whose embed matched no file would otherwise answer every product question from guesswork,
		// with nothing in the log to say why.
		return "", errors.New("the bundled documentation is empty")
	}

	docs := make([]string, 0, len(paths))
	for _, path := range paths {
		text, err := knowledgeFiles.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read %s: %w", path, err)
		}
		docs = append(docs, strings.TrimSpace(string(text)))
	}
	return strings.Join(docs, "\n\n---\n\n"), nil
}
