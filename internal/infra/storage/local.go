// Package storage keeps the bytes of uploaded files on the local disk.
//
// Local disk rather than object storage because the deployment is one machine with a directory on it: an
// S3 client would be a second service to run, a second set of credentials to leak, and a second network
// hop, for a feature whose files are small and written once. The interface is narrow enough that a bucket
// can replace this file without touching the service that uses it.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrTooLarge is returned when a stream exceeds the limit the caller set.
var ErrTooLarge = errors.New("the file is larger than the limit")

// Store writes and reads files under one directory.
type Store struct {
	dir string
}

// New returns a store rooted at dir, creating it if it does not exist.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("the upload directory is empty")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create the upload directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Put writes the stream to a new file named name, refusing to store more than limit bytes.
//
// The file is written beside its final name and renamed into place, so a write that fails halfway leaves no
// half-file that a URL could point at.
func (s *Store) Put(name string, r io.Reader, limit int64) (path string, size int64, sum string, err error) {
	limited := &limitedReader{r: r, left: limit}

	tmp, err := os.CreateTemp(s.dir, ".incoming-*")
	if err != nil {
		return "", 0, "", fmt.Errorf("create a temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", 0, "", fmt.Errorf("write the file: %w", err)
	}

	final := filepath.Join(s.dir, name)
	// 0600: the file is served by this process, which runs as the same user that wrote it, so nothing else
	// on the host has a reason to read it.
	if err = os.Chmod(tmpName, 0o600); err != nil {
		return "", 0, "", fmt.Errorf("set the file mode: %w", err)
	}
	if err = os.Rename(tmpName, final); err != nil {
		return "", 0, "", fmt.Errorf("move the file into place: %w", err)
	}

	return name, written, hex.EncodeToString(hasher.Sum(nil)), nil
}

// Open returns the contents of a file previously written under path.
func (s *Store) Open(path string) (io.ReadSeekCloser, error) {
	// The path came out of the database rather than from a request, but it is still joined here and
	// cleaned: a row written by an older bug must not be able to read outside the directory.
	clean := filepath.Clean(path)
	if clean != path || filepath.IsAbs(clean) || len(clean) > 2 && clean[:2] == ".." {
		return nil, errors.New("invalid storage path")
	}
	//nolint:gosec // the path is validated two lines above: cleaned, relative, and refused if it starts with ..
	return os.Open(filepath.Join(s.dir, clean))
}

// Delete removes a file. A file that is already gone is not an error: the row is the source of truth, and
// deleting a link must succeed whether or not its bytes are still on disk.
func (s *Store) Delete(path string) error {
	clean := filepath.Clean(path)
	if clean != path || filepath.IsAbs(clean) || len(clean) > 2 && clean[:2] == ".." {
		return errors.New("invalid storage path")
	}
	if err := os.Remove(filepath.Join(s.dir, clean)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// limitedReader fails with ErrTooLarge once more than left bytes have been read.
type limitedReader struct {
	r    io.Reader
	left int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n, ErrTooLarge
	}
	return n, err
}
