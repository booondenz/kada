package ua

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/chun/kada-backend/internal/domain"
)

// Detect determines the source platform from the User-Agent
func Detect(userAgent string) domain.Platform {
	ua := strings.ToLower(userAgent)

	// WeChat in-app browser
	if strings.Contains(ua, "micromessenger") {
		return domain.PlatformWechat
	}

	// QQ in-app browser (note the order: check QQ before WeChat, since QQ UAs may also contain MQQBrowser)
	if strings.Contains(ua, "qq/") || strings.Contains(ua, "mqqbrowser") {
		return domain.PlatformQQ
	}

	// Weibo
	if strings.Contains(ua, "weibo") || strings.Contains(ua, "weibo__") {
		return domain.PlatformWeibo
	}

	// Xiaohongshu
	if strings.Contains(ua, "xhs") || strings.Contains(ua, "redapp") {
		return domain.PlatformXiaohongshu
	}

	return domain.PlatformBrowser
}

// crawlerMarkers are the substrings that name a client which is not a person browsing: search engine
// crawlers, SEO crawlers, and the link preview fetchers of the platforms whose fetcher does not wear the
// app's own User-Agent.
//
// The list is deliberately short and deliberately literal. It is not an attempt to recognize every bot -
// it is the set of tokens a browser never sends, so a match cannot cost a real visitor. That constraint is
// exactly what WeChat and QQ fail: their preview fetcher wears the in-app browser's own User-Agent
// (MicroMessenger/..., QQ/...), so a pattern that caught the fetcher would throw the visitor away with it.
// Those two are handled by the guide page instead, which the fetcher fetches and never confirms.
//
// WhatsApp is left out for the same reason - its in-app browser sends WhatsApp/2.x, which is also what its
// preview fetcher sends - so a WhatsApp visit is still counted on the strength of its request alone.
// DingTalk and Feishu are left out because their fetcher tokens are not documented well enough to add
// without risking a pattern that a real client also sends. Both omissions under-count prefetch; neither
// can throw away a visitor, which is the trade this list is built to make.
var crawlerMarkers = []string{
	// Search engines.
	"googlebot", "bingbot", "baiduspider", "yandexbot", "duckduckbot", "applebot", "petalbot",
	"bytespider", "sogou web spider", "360spider", "haosouspider",
	// Link preview fetchers.
	"facebookexternalhit", "twitterbot", "telegrambot", "slackbot", "discordbot", "linkedinbot",
	"skypeuripreview", "embedly", "quora link preview", "vkshare", "pinterestbot", "redditbot",
	// SEO crawlers, which fetch every link they are given.
	"ahrefsbot", "semrushbot", "mj12bot", "dotbot",
	// Tools and headless browsers: a link is fetched without anyone looking at it.
	"curl/", "wget/", "python-requests", "python-urllib", "go-http-client", "okhttp",
	"libwww-perl", "apache-httpclient", "postmanruntime", "insomnia", "headlesschrome",
}

// IsCrawler reports whether the User-Agent names a client that is not a person browsing the web.
//
// An empty User-Agent counts as a crawler: no browser omits one, so a request without it did not come from
// a page somebody is looking at. See crawlerMarkers for what is deliberately not on the list.
func IsCrawler(userAgent string) bool {
	if userAgent == "" {
		return true
	}
	ua := strings.ToLower(userAgent)
	for _, marker := range crawlerMarkers {
		if strings.Contains(ua, marker) {
			return true
		}
	}
	return false
}

// NeedsIntermediatePage reports whether an intermediate guidance page is required (instead of a direct 302)
func NeedsIntermediatePage(platform domain.Platform) bool {
	switch platform {
	case domain.PlatformWechat, domain.PlatformQQ, domain.PlatformXiaohongshu:
		return true
	default:
		return false
	}
}

// PlatformName returns the platform display name
func PlatformName(platform domain.Platform) string {
	switch platform {
	case domain.PlatformWechat:
		return "WeChat"
	case domain.PlatformQQ:
		return "QQ"
	case domain.PlatformWeibo:
		return "Weibo"
	case domain.PlatformXiaohongshu:
		return "Xiaohongshu"
	case domain.PlatformSMS:
		return "SMS"
	default:
		return "Browser"
	}
}

// PlatformTips returns the platform-specific guidance copy
func PlatformTips(platform domain.Platform) string {
	switch platform {
	case domain.PlatformWechat:
		return "Please open the link in WeChat, or tap the menu in the top-right corner and choose \"Open in Browser\""
	case domain.PlatformQQ:
		return "The built-in QQ browser may block page redirects; please open the link in an external browser"
	case domain.PlatformXiaohongshu:
		return "Xiaohongshu does not support direct external links; please copy the link and open it in a browser"
	case domain.PlatformWeibo:
		return "Opening links inside Weibo may be restricted; please open it in a browser"
	default:
		return "Opening the link for you..."
	}
}

// GetDeeplinks builds per-platform deeplink fallback options for the target URL
func GetDeeplinks(targetURL string) []domain.DeepLink {
	// Strip the protocol prefix for use in the intent scheme
	stripped := strings.TrimPrefix(targetURL, "https://")
	stripped = strings.TrimPrefix(stripped, "http://")
	encoded := url.QueryEscape(targetURL)

	links := []domain.DeepLink{
		{
			Name:   "Open directly",
			Scheme: targetURL,
		},
		{
			Name:   "Chrome",
			Scheme: fmt.Sprintf("intent://%s#Intent;scheme=https;package=com.android.chrome;end", stripped),
		},
		{
			Name:   "System browser",
			Scheme: fmt.Sprintf("intent://%s#Intent;scheme=https;end", stripped),
		},
	}

	// In-WeChat navigation: try relaying through a WeChat URL scheme
	_ = encoded // reserved for WeChat-specific schemes

	return links
}
