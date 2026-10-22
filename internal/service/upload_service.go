package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/domain/entity"
	"github.com/booondenz/kada/internal/infra/storage"
)

const (
	// maxUploadBytes is one file's ceiling: a photo or a slide deck, and far below what a video needs -
	// which is the point, since video and audio are out of scope and refused by type before this matters.
	maxUploadBytes = 20 << 20
	// Per-account storage. One allowance, because there is one kind of account: a phone number used to buy a
	// larger one, and the site has no phone numbers any more.
	guestStorageBytes = 50 << 20
)

// allowedExtensions is the whole list of what this service stores, and what each one must claim to be.
//
// The type comes from the extension, but only after the first bytes agree with it: a file named .png that
// starts with a ZIP header is refused, so the extension is a claim that has to hold rather than an
// instruction.
var allowedExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// sniffExcuses are the content types a zip container may be detected as. The Office formats are zips, so
// DetectContentType cannot tell them apart - the extension is what distinguishes them, and the bytes only
// have to be a zip.
var sniffExcuses = map[string]bool{
	"application/zip":              true,
	"application/octet-stream":     true,
	"application/x-zip-compressed": true,
}

// UploadService stores the files behind file links.
type UploadService struct {
	db      *gorm.DB
	store   *storage.Store
	baseURL string
}

func NewUploadService(db *gorm.DB, store *storage.Store, baseURL string) *UploadService {
	return &UploadService{db: db, store: store, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// URL is where a stored file is served from. It is on this domain on purpose: a short link that pointed at
// somebody else's image host is the thing this feature exists to avoid.
func (s *UploadService) URL(u *entity.Upload) string {
	return s.baseURL + "/f/" + u.PublicID
}

// Create stores one file for an account.
//
// The head of the stream is sniffed before anything is written: the extension decides which type the file
// claims to be, and the first bytes decide whether the claim holds.
func (s *UploadService) Create(ctx context.Context, userID int64, filename string, r io.Reader) (*domain.UploadInfo, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	want, ok := allowedExtensions[ext]
	if !ok {
		return nil, fmt.Errorf("%w: only png, jpg, webp, gif, pdf, docx, xlsx and pptx can be stored", domain.ErrUnsupportedFile)
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, errors.New("failed to read the file")
	}
	head = head[:n]

	if !contentMatches(want, http.DetectContentType(head)) {
		return nil, fmt.Errorf("%w: the contents are not %s", domain.ErrUnsupportedFile, want)
	}

	limit := int64(guestStorageBytes)
	used := s.usedBytes(ctx, userID)
	if used >= limit {
		return nil, fmt.Errorf("%w: this account can store %s and it is full", domain.ErrStorageQuota, humanBytes(limit))
	}
	budget := limit - used
	if budget > maxUploadBytes {
		budget = maxUploadBytes
	}

	publicID := newEventID()
	path, size, sum, err := s.store.Put(publicID+ext, io.MultiReader(bytes.NewReader(head), r), budget)
	if errors.Is(err, storage.ErrTooLarge) {
		return nil, fmt.Errorf("%w: one file may be at most %s, and this account has %s left",
			domain.ErrStorageQuota, humanBytes(maxUploadBytes), humanBytes(budget))
	}
	if err != nil {
		return nil, errors.New("failed to store the file")
	}

	row := entity.Upload{
		PublicID:    publicID,
		UserID:      &userID,
		Filename:    safeName(filename),
		ContentType: want,
		Size:        size,
		SHA256:      sum,
		StoragePath: path,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// The bytes are on disk but nothing points at them; the orphan sweep will take them.
		return nil, errors.New("failed to record the upload")
	}

	return &domain.UploadInfo{
		ID:          row.ID,
		URL:         s.URL(&row),
		Filename:    row.Filename,
		ContentType: row.ContentType,
		Size:        row.Size,
		CreatedAt:   row.CreatedAt,
	}, nil
}

// ByID returns an upload the account owns, which is what a link may be created from.
func (s *UploadService) ByID(ctx context.Context, id, userID int64) (*entity.Upload, error) {
	var row entity.Upload
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&row).Error; err != nil {
		return nil, domain.ErrLinkNotFound
	}
	return &row, nil
}

// Open returns a stored file's contents.
func (s *UploadService) Open(ctx context.Context, publicID string) (*entity.Upload, io.ReadSeekCloser, error) {
	var row entity.Upload
	if err := s.db.WithContext(ctx).Where("public_id = ?", publicID).First(&row).Error; err != nil {
		return nil, nil, domain.ErrLinkNotFound
	}
	file, err := s.store.Open(row.StoragePath)
	if err != nil {
		return nil, nil, err
	}
	return &row, file, nil
}

// SweepOrphans deletes uploads no link refers to, once they are older than the grace period.
//
// Two things produce orphans: an upload whose link was deleted, and an upload whose link was never created.
// Sweeping rather than deleting at the point of deletion is what keeps the link service out of the file
// business - and the grace period is what stops a link that is being created right now from losing its file
// to a sweep that ran between the upload and the create.
func (s *UploadService) SweepOrphans(ctx context.Context, olderThan time.Duration) (int, error) {
	var rows []entity.Upload
	err := s.db.WithContext(ctx).
		Where("created_at < ? AND id NOT IN (SELECT upload_id FROM links WHERE upload_id IS NOT NULL)", time.Now().UTC().Add(-olderThan)).
		Limit(200).
		Find(&rows).Error
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, row := range rows {
		if err := s.db.WithContext(ctx).Delete(&entity.Upload{}, row.ID).Error; err != nil {
			log.Printf("delete orphan upload %d failed: %v", row.ID, err)
			continue
		}
		if err := s.store.Delete(row.StoragePath); err != nil {
			log.Printf("delete the file of orphan upload %d failed: %v", row.ID, err)
		}
		removed++
	}
	return removed, nil
}

// usedBytes is how much an account has stored.
func (s *UploadService) usedBytes(ctx context.Context, userID int64) int64 {
	var total int64
	if err := s.db.WithContext(ctx).Model(&entity.Upload{}).
		Where("user_id = ?", userID).
		Select("COALESCE(SUM(size), 0)").Scan(&total).Error; err != nil {
		log.Printf("read the storage used by account %d failed: %v", userID, err)
		return 0
	}
	return total
}

// contentMatches reports whether the sniffed type is the type the extension claimed.
func contentMatches(want, sniffed string) bool {
	if want == sniffed {
		return true
	}
	// The Office formats are zip containers and sniff as generic zip; the extension is what separates them.
	return strings.Contains(want, "openxmlformats") && sniffExcuses[sniffed]
}

// safeName makes a client-supplied filename safe to store and to show, without collapsing it to an
// extension: the owner should still recognize the file they uploaded.
func safeName(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

// humanBytes renders a size the way the refusal has to say it.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%d GB", n>>30)
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
