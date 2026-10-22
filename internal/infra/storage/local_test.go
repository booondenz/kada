package storage

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()

	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestPutThenOpenRoundTrips(t *testing.T) {
	s := newStore(t)
	contents := bytes.Repeat([]byte("kada"), 100)

	path, size, sum, err := s.Put("abc.png", bytes.NewReader(contents), 1<<20)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if path != "abc.png" || size != int64(len(contents)) || len(sum) != 64 {
		t.Fatalf("Put returned %q %d %q", path, size, sum)
	}

	file, err := s.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = file.Close() }()

	read, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(read, contents) {
		t.Error("the file read back is not the file written")
	}
}

// A stream over the limit is refused, and the refusal leaves nothing behind: a half-written file that a URL
// could reach is worse than a refused upload.
func TestPutRefusesAStreamOverTheLimit(t *testing.T) {
	s := newStore(t)
	tooMuch := bytes.Repeat([]byte("x"), 2048)

	_, _, _, err := s.Put("big.png", bytes.NewReader(tooMuch), 1024)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put error = %v, want ErrTooLarge", err)
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a refused upload left %d files behind", len(entries))
	}
}

// The path comes out of the database, but the store still refuses to be walked out of: a row written by an
// older bug must not be able to read the filesystem.
func TestOpenRefusesAPathOutsideTheDirectory(t *testing.T) {
	s := newStore(t)

	for _, path := range []string{"../secret", "/etc/passwd", "a/../../b"} {
		if _, err := s.Open(path); err == nil {
			t.Errorf("Open(%q) succeeded", path)
		}
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	s := newStore(t)

	path, _, _, err := s.Put("gone.png", strings.NewReader("x"), 16)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(path); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := s.Delete(path); err != nil {
		t.Errorf("deleting a file that is already gone must not fail: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, path)); !os.IsNotExist(err) {
		t.Error("the file is still there")
	}
}
