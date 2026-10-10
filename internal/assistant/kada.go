package assistant

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/service"
)

// Kada is the assistant's view of this application's own services: the operations the model may perform on
// behalf of the signed-in user.
//
// The Python service reached these over HTTP *as the user*, carrying the JWT the gateway forwarded - and
// that is exactly why its business tools could not live in the MCP subprocess, whose one long-lived stdio
// connection has no request to belong to. In this process there is no second identity to carry: these
// tools call the same services the HTTP handlers call, with the user id the handler already has.
type Kada struct {
	links     *service.LinkService
	analytics overviewService
}

// overviewService is the slice of the analytics service the assistant needs.
type overviewService interface {
	Overview(ctx context.Context, userID int64) (service.Overview, error)
}

// NewKada builds the assistant's handle on the application's services.
func NewKada(links *service.LinkService, analytics overviewService) *Kada {
	return &Kada{links: links, analytics: analytics}
}

// LinkOverview reports the user's totals as JSON.
//
// JSON rather than a sentence because that is what the Python tool handed the model - it returned the body
// of GET /api/analytics/overview - and the model reads this shape the same way.
func (k *Kada) LinkOverview(ctx context.Context, userID int64) (string, error) {
	totals, err := k.analytics.Overview(ctx, userID)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(totals)
	if err != nil {
		return "", fmt.Errorf("failed to encode the overview: %w", err)
	}
	return string(body), nil
}

// CreateShortLink creates a link for the user and describes it the way the model relays it back.
func (k *Kada) CreateShortLink(ctx context.Context, userID int64, url string) (string, error) {
	link, err := k.links.Create(ctx, userID, domain.CreateLinkRequest{OriginalURL: url})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("短链接创建成功：\n短链接：%s\n原链接：%s\n短码：%s",
		link.ShortURL, link.OriginalURL, link.ShortCode), nil
}
