package domain

import (
	"errors"
	"slices"
	"time"
)

var ErrLinkNotFound = errors.New("link not found")

// RateLimitError is a refusal caused by a quota rather than by anything wrong with the request.
//
// It is a type rather than a sentinel because the caller needs two things a bare error cannot carry: the
// message to show the user, and how long to wait. The HTTP handler maps it to 429 + Retry-After; every
// other failure from the auth service is the caller's fault and stays a 400.
type RateLimitError struct {
	Message           string
	RetryAfterSeconds int
}

func (e *RateLimitError) Error() string { return e.Message }

// ==================== Request/Response models ====================

// ---- Auth ----

// SendSMSRequest asks for a login code.
//
// The captcha fields are required, not optional: the point of the challenge is that no SMS can be
// triggered without a human having solved one, and an optional challenge is one an attacker simply omits.
type SendSMSRequest struct {
	Phone       string `json:"phone" binding:"required"`
	CaptchaID   string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
}

// CaptchaResponse is the graphical challenge: an opaque id to send back with the answer, and the image
// as a data URI so the client needs no second request and no separate image endpoint.
type CaptchaResponse struct {
	ID    string `json:"captcha_id"`
	Image string `json:"image"`
}

type LoginByPhoneRequest struct {
	Phone string `json:"phone" binding:"required"`
	Code  string `json:"code" binding:"required"`
}

// RegisterRequest creates an account: a verification code that proves the number, and the password that is
// used to sign in from then on.
//
// Sending the code still costs money, which is the whole reason the password exists: one message at
// registration instead of one at every sign-in.
type RegisterRequest struct {
	Phone    string `json:"phone" binding:"required"`
	Code     string `json:"code" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// PasswordLoginRequest signs in with the password chosen at registration.
type PasswordLoginRequest struct {
	Phone    string `json:"phone" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// ErrInvalidCredentials is returned for every way a password sign-in can fail to match: an unknown number, a
// wrong password, and an account that has no password yet. They are one error on purpose - telling them
// apart turns the endpoint into a way to ask which numbers have accounts here.
var ErrInvalidCredentials = errors.New("the phone number or password is incorrect")

// ErrAlreadyRegistered is returned when a number that already has a password is registered again. It is a
// distinct error because the caller can act on it: the account exists, so sign in instead.
var ErrAlreadyRegistered = errors.New("this phone number already has an account, please sign in")

type AuthResponse struct {
	Token string   `json:"token"`
	User  UserInfo `json:"user"`
}

// UserInfo is what a caller learns about an account: the phone number and nothing else.
//
// The phone is the whole of sign-in - the identifier, the second factor and the unique key - so there is
// no name to display, no email to edit and no avatar to render. Everything in this struct is published to
// every client and cached in localStorage, which is why its shape is pinned by a test.
type UserInfo struct {
	ID    int64   `json:"id"`
	Phone *string `json:"phone"`
}

// ---- Links ----

type CreateLinkRequest struct {
	OriginalURL string  `json:"original_url" binding:"required,url"`
	ShortCode   *string `json:"short_code"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	ImageURL    *string `json:"image_url"`
	Domain      *string `json:"domain"`
	Password    *string `json:"password"`
	ExpiresAt   *string `json:"expires_at"`
	FolderID    *int64  `json:"folder_id"`
	TagIDs      []int64 `json:"tag_ids"`
	UTMSource   *string `json:"utm_source"`
	UTMMedium   *string `json:"utm_medium"`
	UTMCampaign *string `json:"utm_campaign"`
	UTMTerm     *string `json:"utm_term"`
	UTMContent  *string `json:"utm_content"`
	IosURL      *string `json:"ios_url"`
	AndroidURL  *string `json:"android_url"`
}

type UpdateLinkRequest struct {
	OriginalURL *string `json:"original_url"`
	ShortCode   *string `json:"short_code"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	ImageURL    *string `json:"image_url"`
	Domain      *string `json:"domain"`
	Password    *string `json:"password"`
	ExpiresAt   *string `json:"expires_at"`
	IsActive    *bool   `json:"is_active"`
	FolderID    *int64  `json:"folder_id"`
	TagIDs      []int64 `json:"tag_ids"`
	UTMSource   *string `json:"utm_source"`
	UTMMedium   *string `json:"utm_medium"`
	UTMCampaign *string `json:"utm_campaign"`
	UTMTerm     *string `json:"utm_term"`
	UTMContent  *string `json:"utm_content"`
	IosURL      *string `json:"ios_url"`
	AndroidURL  *string `json:"android_url"`
}

type LinkTagInfo struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// LinkInfo is a link as the API and the cache see it. It deliberately has no field able to hold the
// password hash: the struct is encoded into Redis by CacheService.SetLink and into API responses, so a
// hash field would put it in both. HasPassword carries the only fact either needs. users.password_hash
// is tagged json:"-" for the same reason (see TestUserHidesPasswordHash).
type LinkInfo struct {
	ID          int64         `json:"id"`
	ShortCode   string        `json:"short_code"`
	ShortURL    string        `json:"short_url"`
	OriginalURL string        `json:"original_url"`
	Title       *string       `json:"title"`
	Description *string       `json:"description"`
	ImageURL    *string       `json:"image_url"`
	Domain      string        `json:"domain"`
	ClickCount  int64         `json:"click_count"`
	IsActive    bool          `json:"is_active"`
	ExpiresAt   *time.Time    `json:"expires_at"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	FolderID    *int64        `json:"folder_id"`
	FolderName  *string       `json:"folder_name"`
	Tags        []LinkTagInfo `json:"tags"`
	HasPassword bool          `json:"has_password"`
	UTMSource   *string       `json:"utm_source,omitempty"`
	UTMMedium   *string       `json:"utm_medium,omitempty"`
	UTMCampaign *string       `json:"utm_campaign,omitempty"`
	UTMTerm     *string       `json:"utm_term,omitempty"`
	UTMContent  *string       `json:"utm_content,omitempty"`
	IosURL      *string       `json:"ios_url,omitempty"`
	AndroidURL  *string       `json:"android_url,omitempty"`
}

type PaginatedLinks struct {
	Links      []LinkInfo `json:"links"`
	TotalCount int64      `json:"total_count"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
}

// ==================== Platform detection ====================

type Platform string

const (
	PlatformBrowser     Platform = "browser"
	PlatformWechat      Platform = "wechat"
	PlatformQQ          Platform = "qq"
	PlatformWeibo       Platform = "weibo"
	PlatformXiaohongshu Platform = "xiaohongshu"
	PlatformSMS         Platform = "sms"
	PlatformUnknown     Platform = "unknown"
)

// ==================== Click events ====================

// ClickKind is what a recorded click row is evidence of.
//
// It exists because a request to a short link is not the same thing as a person opening it. WeChat and QQ
// fetch a link to build the preview card before anyone taps it, and that fetcher sends the in-app
// browser's own User-Agent - so the question "was this a person?" cannot be answered by looking at the
// User-Agent, and the guide page answers it instead by reporting from JavaScript, which a fetcher that
// only reads HTML never runs.
type ClickKind string

const (
	// ClickVisit is evidence that a person opened the link, and the only kind that moves a counter.
	//
	// Two things produce it. A client that is sent straight to the target never runs any of our
	// JavaScript, so that request is the only evidence there will ever be; and a client that is served the
	// guide page confirms itself with POST /r/:code/visit once its scripts run.
	ClickVisit ClickKind = "visit"

	// ClickRequest is a request that is not evidence of a person: the guide page was served and nothing
	// confirmed it, or the User-Agent names a crawler. The row is kept and labeled so that the amount of
	// prefetch stays measurable.
	ClickRequest ClickKind = "request"

	// ClickAction is an interaction on the guide page - a copy, a QR reveal, an attempt to open the
	// browser. It happens after the visit was confirmed, so it is recorded but never counted: a person who
	// taps two buttons is one visit, not two.
	ClickAction ClickKind = "action"
)

// Action is one interaction on the guide page: the thing a visitor did after the link had already opened.
//
// The set is closed, and the endpoint that receives one rejects anything outside it. That is the point:
// the action used to be free text the client chose, stored in the referer column, so a caller could write
// whatever it liked into a column that is read as "where this visitor came from".
type Action string

const (
	ActionOpenLink    Action = "open_link"
	ActionOpenBrowser Action = "open_browser"
	ActionCopyLink    Action = "copy_link"
	ActionQRView      Action = "qr_view"
)

// AllActions is every interaction the guide page can report, and therefore everything the endpoint accepts.
// Both directions matter: an action defined here that the page never sends is dead vocabulary, and one the
// page sends without being defined here is rejected in production by a 400 no visitor ever sees.
var AllActions = []Action{ActionOpenLink, ActionOpenBrowser, ActionCopyLink, ActionQRView}

// Known reports whether this is one of the actions the application defines.
func (a Action) Known() bool {
	return slices.Contains(AllActions, a)
}

// ClickEvent is one click on a short link, as the application sees it: the HTTP request has been
// classified, and the question left is whether it will be counted.
//
// It is a struct rather than a parameter list because every field but LinkID is a string, and four of them
// sit next to each other - a caller that swapped two would compile and be wrong.
type ClickEvent struct {
	LinkID    int64
	IP        string
	UserAgent string
	Platform  Platform
	Kind      ClickKind
	// Action is set only when Kind is ClickAction. It is its own field rather than a prefix on the referer,
	// which is where it used to live: a report that reads referers had to know some of them were not.
	Action  Action
	Referer string
}

// DeepLink represents one deeplink fallback option
type DeepLink struct {
	Name   string `json:"name"`   // option name, e.g. "Chrome Intent"
	Scheme string `json:"scheme"` // URL scheme, e.g. "intent://..."
}

// ==================== Folders ====================

type Folder struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	LinkCount int64     `json:"link_count,omitempty"`
}

type CreateFolderRequest struct {
	Name string `json:"name" binding:"required"`
}

// ==================== Tags ====================

type Tag struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTagRequest struct {
	Name  string  `json:"name" binding:"required"`
	Color *string `json:"color"`
}

// ==================== Domains ====================

type Domain struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"user_id"`
	Name             string     `json:"name"`
	Verified         bool       `json:"verified"`
	VerifiedAt       *time.Time `json:"verified_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	VerificationCode string     `json:"verification_code,omitempty"`
}

type CreateDomainRequest struct {
	Name string `json:"name" binding:"required"`
}

// ==================== UTM templates ====================

type UTMTemplate struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name"`
	UTMSource   *string   `json:"utm_source"`
	UTMMedium   *string   `json:"utm_medium"`
	UTMCampaign *string   `json:"utm_campaign"`
	UTMTerm     *string   `json:"utm_term"`
	UTMContent  *string   `json:"utm_content"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateUTMTemplateRequest struct {
	Name        string  `json:"name" binding:"required"`
	UTMSource   *string `json:"utm_source"`
	UTMMedium   *string `json:"utm_medium"`
	UTMCampaign *string `json:"utm_campaign"`
	UTMTerm     *string `json:"utm_term"`
	UTMContent  *string `json:"utm_content"`
}

// ==================== API Token ====================

type APIToken struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Name      string     `json:"name"`
	LastUsed  *time.Time `json:"last_used"`
	CreatedAt time.Time  `json:"created_at"`
}

type CreateAPITokenRequest struct {
	Name string `json:"name" binding:"required"`
}

type CreateAPITokenResponse struct {
	Token    string   `json:"token"`
	APIToken APIToken `json:"api_token"`
}

// ==================== Link preview ====================

type LinkPreviewRequest struct {
	URL string `json:"url" binding:"required,url"`
}

type LinkPreview struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
	FaviconURL  string `json:"favicon_url"`
}
