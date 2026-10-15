package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/middleware"
)

// AuthService is the authentication service interface (mockable for tests).
type AuthService interface {
	GenerateCaptcha(ctx context.Context, ip string) (*domain.CaptchaResponse, error)
	SendSMSCode(ctx context.Context, phone, ip, captchaID, captchaAnswer string) error
	Register(ctx context.Context, phone, code, password string) (*domain.AuthResponse, error)
	LoginWithPassword(ctx context.Context, phone, password string) (*domain.AuthResponse, error)
	LoginByPhone(ctx context.Context, phone, code string) (*domain.AuthResponse, error)
	GetUserByID(ctx context.Context, userID int64) (*domain.UserInfo, error)
}

type Handler struct {
	svc AuthService
}

func NewHandler(svc AuthService) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes wires the auth endpoints.
//
// Signing in is phone plus password, and the password is set once at registration. That split is what keeps
// the SMS bill bounded: a message is sent to prove the number belongs to the caller, not on every sign-in.
// Registration writes the code hash and the password hash, and nothing else - the email/password and WeChat
// routes are gone rather than hidden, because an endpoint nobody uses is still an endpoint that can be
// attacked.
//
// `login-by-phone` stays, and is the only way into an account that has no password yet - which is every
// account created before this flow existed. It is also how such an account gets one: registering the same
// number sets the password, since the code proves exactly what the SMS login proved.
//
// `PATCH /me` went the same way once the profile became the phone number. A phone cannot be changed
// without verifying the new one (a flow this app does not have) and there is no other field left to write,
// so the route would have accepted a body and changed nothing.
func (h *Handler) RegisterRoutes(r *gin.RouterGroup, authMW gin.HandlerFunc, strictMW ...gin.HandlerFunc) {
	// Public routes (strict rate limiting can be applied).
	//
	// The strict limiter is not decoration on the two password routes: they are the only place where
	// guessing pays, and twenty attempts a minute per address is what stands between a password and a
	// script.
	public := r.Group("")
	if len(strictMW) > 0 && strictMW[0] != nil {
		public.Use(strictMW[0])
	}
	public.GET("/auth/captcha", h.Captcha)
	public.POST("/auth/send-sms-code", h.SendSMSCode)
	public.POST("/auth/register", h.Register)
	public.POST("/auth/login", h.Login)
	public.POST("/auth/login-by-phone", h.LoginByPhone)

	// Routes that require authentication.
	auth := r.Group("").Use(authMW)
	auth.GET("/me", h.GetMe)
}

// Captcha issues the graphical challenge that SendSMSCode requires.
//
// It is a GET because it has no side effect on the caller's session and is safe to retry; the row it
// creates is one-time and expires in five minutes either way.
func (h *Handler) Captcha(c *gin.Context) {
	challenge, err := h.svc.GenerateCaptcha(c.Request.Context(), middleware.RealIP(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// The image is a data URI, and the challenge is per-request: a cached copy would hand the same
	// unsolved puzzle to two people behind one proxy.
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"captcha_id": challenge.ID, "image": challenge.Image})
}

// rateLimited answers a quota refusal: 429 with the wait, which is what turns "come back later" into something
// a client can count down.
//
// Both the SMS quotas and the password bound are answered here, so the two cannot drift into different shapes.
// Reporting a quota as 500 - which this handler used to do - tells a client to retry a request that will keep
// failing, and tells the operator to look for a server fault that does not exist.
func rateLimited(c *gin.Context, limited *domain.RateLimitError) {
	if limited.RetryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(limited.RetryAfterSeconds))
	}
	c.JSON(http.StatusTooManyRequests, gin.H{
		"error":               limited.Message,
		"retry_after_seconds": limited.RetryAfterSeconds,
	})
}

// SendSMSCode sends an SMS verification code.
func (h *Handler) SendSMSCode(c *gin.Context) {
	var req domain.SendSMSRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid phone number and the graphical verification code are required"})
		return
	}

	// The IP is taken from the same place the rate limiter takes it: X-Real-IP, which nginx rewrites from
	// $remote_addr. Reading X-Forwarded-For here instead would let a caller choose which quota bucket to
	// be counted in by sending a header of their own.
	if err := h.svc.SendSMSCode(c.Request.Context(), req.Phone, middleware.RealIP(c), req.CaptchaID, req.CaptchaCode); err != nil {
		// A quota refusal is 429 with a wait time; a wrong captcha or a malformed number is 400.
		var limited *domain.RateLimitError
		if errors.As(err, &limited) {
			rateLimited(c, limited)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "verification code sent",
		"phone":   req.Phone,
	})
}

// Register creates the account: the verification code proves the number, and the password is what is used
// to sign in from then on.
func (h *Handler) Register(c *gin.Context) {
	var req domain.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a phone number, the verification code and a password are required"})
		return
	}

	resp, err := h.svc.Register(c.Request.Context(), req.Phone, req.Code, req.Password)
	if err != nil {
		// The one error worth its own status: the number already has an account with a password, so this is
		// not a failed registration but a request to sign in instead - and a 400 would say "your input was
		// wrong" about input that was right.
		if errors.Is(err, domain.ErrAlreadyRegistered) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": resp.Token, "user": resp.User})
}

// Login signs in with the password chosen at registration.
func (h *Handler) Login(c *gin.Context) {
	var req domain.PasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phone number and password are required"})
		return
	}

	resp, err := h.svc.LoginWithPassword(c.Request.Context(), req.Phone, req.Password)
	if err != nil {
		// 429 first, because a number that has failed too often gets the same answer whatever address it is
		// tried from and whether or not it has an account - which is the point of counting the number.
		var limited *domain.RateLimitError
		if errors.As(err, &limited) {
			rateLimited(c, limited)
			return
		}
		// Then 401 for "these credentials do not match", 400 for a malformed number or a service failure. The
		// message is the same whichever way the credentials failed, so the response cannot be used to ask
		// which numbers have accounts.
		if errors.Is(err, domain.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": resp.Token, "user": resp.User})
}

// LoginByPhone logs in with phone number + verification code.
func (h *Handler) LoginByPhone(c *gin.Context) {
	var req domain.LoginByPhoneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "phone number and verification code are required"})
		return
	}

	resp, err := h.svc.LoginByPhone(c.Request.Context(), req.Phone, req.Code)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": resp.Token, "user": resp.User})
}

// GetMe returns the current user's profile.
func (h *Handler) GetMe(c *gin.Context) {
	userID := middleware.GetUserID(c)

	user, err := h.svc.GetUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": user})
}
