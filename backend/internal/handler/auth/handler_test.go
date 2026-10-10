package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/chun/kada-backend/internal/domain"
)

// mockAuthService is used for handler tests.
type mockAuthService struct {
	generateCaptcha func(ctx context.Context, ip string) (*domain.CaptchaResponse, error)
	sendSMSCode     func(ctx context.Context, phone, ip, captchaID, captchaAnswer string) error
	register        func(ctx context.Context, phone, code, password string) (*domain.AuthResponse, error)
	loginWithPass   func(ctx context.Context, phone, password string) (*domain.AuthResponse, error)
	loginByPhone    func(ctx context.Context, phone, code string) (*domain.AuthResponse, error)
	getUserByID     func(ctx context.Context, userID int64) (*domain.UserInfo, error)
}

func (m *mockAuthService) GenerateCaptcha(ctx context.Context, ip string) (*domain.CaptchaResponse, error) {
	if m.generateCaptcha != nil {
		return m.generateCaptcha(ctx, ip)
	}
	return &domain.CaptchaResponse{ID: "captcha-id", Image: "data:image/svg+xml;base64,PHN2Zy8+"}, nil
}

func (m *mockAuthService) SendSMSCode(ctx context.Context, phone, ip, captchaID, captchaAnswer string) error {
	if m.sendSMSCode != nil {
		return m.sendSMSCode(ctx, phone, ip, captchaID, captchaAnswer)
	}
	return nil
}

func (m *mockAuthService) Register(ctx context.Context, phone, code, password string) (*domain.AuthResponse, error) {
	if m.register != nil {
		return m.register(ctx, phone, code, password)
	}
	return &domain.AuthResponse{Token: "test-token", User: domain.UserInfo{ID: 1}}, nil
}

func (m *mockAuthService) LoginWithPassword(ctx context.Context, phone, password string) (*domain.AuthResponse, error) {
	if m.loginWithPass != nil {
		return m.loginWithPass(ctx, phone, password)
	}
	return &domain.AuthResponse{Token: "test-token", User: domain.UserInfo{ID: 1}}, nil
}

func (m *mockAuthService) LoginByPhone(ctx context.Context, phone, code string) (*domain.AuthResponse, error) {
	if m.loginByPhone != nil {
		return m.loginByPhone(ctx, phone, code)
	}
	return &domain.AuthResponse{Token: "test-token", User: domain.UserInfo{ID: 1}}, nil
}

func (m *mockAuthService) GetUserByID(ctx context.Context, userID int64) (*domain.UserInfo, error) {
	if m.getUserByID != nil {
		return m.getUserByID(ctx, userID)
	}
	return &domain.UserInfo{ID: userID, Phone: strPtr("13800138000")}, nil
}

func strPtr(s string) *string { return &s }

func setupTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r
}

// postJSON drives one handler with a JSON body.
func postJSON(t *testing.T, r *gin.Engine, path string, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ========== Captcha ==========

func TestCaptcha_ReturnsIDAndImage(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	r.GET("/auth/captcha", h.Captcha)

	req := httptest.NewRequest("GET", "/auth/captcha", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.CaptchaResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.ID == "" {
		t.Error("expected a captcha id in the response")
	}
	if resp.Image == "" {
		t.Error("expected the image data URI in the response")
	}
	// A cached challenge would be handed to everyone behind the same proxy.
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

// The captcha is keyed by the caller's address, and that address has to come from X-Real-IP (what nginx
// rewrites) rather than X-Forwarded-For (what a caller can invent).
func TestCaptcha_UsesRealIPHeader(t *testing.T) {
	var seen string
	h := NewHandler(&mockAuthService{
		generateCaptcha: func(_ context.Context, ip string) (*domain.CaptchaResponse, error) {
			seen = ip
			return &domain.CaptchaResponse{ID: "id", Image: "data:,"}, nil
		},
	})
	r := setupTestRouter(h)
	r.GET("/auth/captcha", h.Captcha)

	req := httptest.NewRequest("GET", "/auth/captcha", nil)
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if seen != "203.0.113.9" {
		t.Errorf("service saw ip %q, want %q (X-Real-IP, not X-Forwarded-For)", seen, "203.0.113.9")
	}
}

// ========== SendSMSCode ==========

func TestSendSMSCode_InvalidJSON(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	r.POST("/auth/send-sms-code", h.SendSMSCode)

	req := httptest.NewRequest("POST", "/auth/send-sms-code", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// The captcha fields are part of the request contract: a request without them must not reach the service
// at all, or the challenge would be optional in practice.
func TestSendSMSCode_RequiresCaptchaFields(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	r.POST("/auth/send-sms-code", h.SendSMSCode)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing everything", map[string]string{}},
		{"missing phone", map[string]string{"captcha_id": "id", "captcha_code": "ABCD"}},
		{"missing captcha id", map[string]string{"phone": "13800138000", "captcha_code": "ABCD"}},
		{"missing captcha code", map[string]string{"phone": "13800138000", "captcha_id": "id"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := postJSON(t, r, "/auth/send-sms-code", tt.body); w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSendSMSCode_Success(t *testing.T) {
	var gotIP, gotCaptcha string
	h := NewHandler(&mockAuthService{
		sendSMSCode: func(_ context.Context, _, ip, captchaID, _ string) error {
			gotIP, gotCaptcha = ip, captchaID
			return nil
		},
	})
	r := setupTestRouter(h)
	r.POST("/auth/send-sms-code", h.SendSMSCode)

	req := httptest.NewRequest("POST", "/auth/send-sms-code",
		bytes.NewReader([]byte(`{"phone":"13800138000","captcha_id":"abc","captcha_code":"A3B7"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Real-IP", "198.51.100.7")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp["message"] != "verification code sent" {
		t.Errorf("expected message 'verification code sent', got %q", resp["message"])
	}
	if gotIP != "198.51.100.7" {
		t.Errorf("service saw ip %q, want %q", gotIP, "198.51.100.7")
	}
	if gotCaptcha != "abc" {
		t.Errorf("service saw captcha id %q, want %q", gotCaptcha, "abc")
	}
}

// A refused send must be distinguishable: a bad captcha is the caller's mistake (400), a quota is a wait
// (429 + Retry-After). Collapsing both into 500 - which is what this handler used to do - hides the
// difference from both the user and the operator.
func TestSendSMSCode_ErrorMapping(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantStatus   int
		wantRetry    string
		wantContains string
	}{
		{
			name:         "wrong captcha is a bad request",
			err:          errors.New("the graphical verification code is incorrect or has expired"),
			wantStatus:   http.StatusBadRequest,
			wantContains: "graphical verification code",
		},
		{
			name:         "no sms service is a bad request, not a crash",
			err:          errors.New("failed to send SMS: missing Aliyun AccessKey pair"),
			wantStatus:   http.StatusBadRequest,
			wantContains: "failed to send SMS",
		},
		{
			name:         "a quota is 429 with a wait time",
			err:          &domain.RateLimitError{Message: "too many requests, please try again in 60 seconds", RetryAfterSeconds: 60},
			wantStatus:   http.StatusTooManyRequests,
			wantRetry:    "60",
			wantContains: "try again in 60 seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&mockAuthService{
				sendSMSCode: func(context.Context, string, string, string, string) error { return tt.err },
			})
			r := setupTestRouter(h)
			r.POST("/auth/send-sms-code", h.SendSMSCode)

			w := postJSON(t, r, "/auth/send-sms-code",
				map[string]string{"phone": "13800138000", "captcha_id": "id", "captcha_code": "A3B7"})

			if w.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
			if got := w.Header().Get("Retry-After"); got != tt.wantRetry {
				t.Errorf("Retry-After = %q, want %q", got, tt.wantRetry)
			}
			if !bytes.Contains(w.Body.Bytes(), []byte(tt.wantContains)) {
				t.Errorf("body %s does not contain %q", w.Body.String(), tt.wantContains)
			}
		})
	}
}

// ========== LoginByPhone ==========

func TestLoginByPhone_MissingFields(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	r.POST("/auth/login-by-phone", h.LoginByPhone)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing phone", map[string]string{"code": "123456"}},
		{"missing code", map[string]string{"phone": "13800138000"}},
		{"empty body", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := postJSON(t, r, "/auth/login-by-phone", tt.body); w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestLoginByPhone_Success(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	r.POST("/auth/login-by-phone", h.LoginByPhone)

	w := postJSON(t, r, "/auth/login-by-phone", map[string]string{"phone": "13800138000", "code": "123456"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected token in response")
	}
}

// ========== Register and password login ==========

func TestRegister_RequiresEveryField(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	r.POST("/auth/register", h.Register)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"empty body", map[string]string{}},
		{"missing phone", map[string]string{"code": "123456", "password": "correct-horse"}},
		{"missing code", map[string]string{"phone": "13800138000", "password": "correct-horse"}},
		{"missing password", map[string]string{"phone": "13800138000", "code": "123456"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := postJSON(t, r, "/auth/register", tt.body); w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// A number that already has a password is a 409, not a 400: the request was well formed and the answer is
// "sign in instead". Reporting it as bad input would send the user back to a form that keeps failing.
func TestRegister_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"an account that already has a password", domain.ErrAlreadyRegistered, http.StatusConflict},
		{"a code that does not match", errors.New("invalid or expired verification code"), http.StatusBadRequest},
		{"a password below the floor", errors.New("the password must be at least 8 characters"), http.StatusBadRequest},
		{"a failed write", errors.New("registration failed, please try again later"), http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&mockAuthService{
				register: func(context.Context, string, string, string) (*domain.AuthResponse, error) {
					return nil, tt.err
				},
			})
			r := setupTestRouter(h)
			r.POST("/auth/register", h.Register)

			w := postJSON(t, r, "/auth/register",
				map[string]string{"phone": "13800138000", "code": "123456", "password": "correct-horse"})
			if w.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestRegister_Success(t *testing.T) {
	var gotPhone, gotCode, gotPassword string
	h := NewHandler(&mockAuthService{
		register: func(_ context.Context, phone, code, password string) (*domain.AuthResponse, error) {
			gotPhone, gotCode, gotPassword = phone, code, password
			return &domain.AuthResponse{Token: "registered-token", User: domain.UserInfo{ID: 9}}, nil
		},
	})
	r := setupTestRouter(h)
	r.POST("/auth/register", h.Register)

	w := postJSON(t, r, "/auth/register",
		map[string]string{"phone": "13800138000", "code": "123456", "password": "correct-horse"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Token != "registered-token" {
		t.Errorf("token = %q, want the one the service issued", resp.Token)
	}
	if gotPhone != "13800138000" || gotCode != "123456" || gotPassword != "correct-horse" {
		t.Errorf("the service saw phone=%q code=%q password=%q", gotPhone, gotCode, gotPassword)
	}
}

// The password reaches the service unmodified: hashing it here would mean the service could only ever
// compare what it was handed, and the cost of bcrypt would land on the HTTP layer.
func TestLogin_RequiresBothFields(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	r.POST("/auth/login", h.Login)

	for _, body := range []map[string]string{{}, {"phone": "13800138000"}, {"password": "correct-horse"}} {
		if w := postJSON(t, r, "/auth/login", body); w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for %v, got %d", body, w.Code)
		}
	}
}

// One 401 for every way the credentials can fail. A different status or message for "no such number" would
// turn the endpoint into a way to ask which numbers have accounts here.
func TestLogin_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"the credentials do not match", domain.ErrInvalidCredentials, http.StatusUnauthorized},
		{"a malformed number", errors.New("invalid phone number format"), http.StatusBadRequest},
		{"a failed read", errors.New("login failed, please try again later"), http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&mockAuthService{
				loginWithPass: func(context.Context, string, string) (*domain.AuthResponse, error) {
					return nil, tt.err
				},
			})
			r := setupTestRouter(h)
			r.POST("/auth/login", h.Login)

			if w := postJSON(t, r, "/auth/login",
				map[string]string{"phone": "13800138000", "password": "correct-horse"}); w.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestLogin_Success(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	r.POST("/auth/login", h.Login)

	w := postJSON(t, r, "/auth/login", map[string]string{"phone": "13800138000", "password": "correct-horse"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp domain.AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected a token in the response")
	}
}

// ========== Routes ==========

// The email routes must be gone, not merely unused: an endpoint nobody links to is an attack surface with
// no test that would notice it breaking. The password routes are the opposite case - they are what the
// sign-in page calls now - so both lists are asserted together.
func TestRegisterRoutes_EmailRoutesAreGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api")
	NewHandler(&mockAuthService{}).RegisterRoutes(v1, func(c *gin.Context) { c.Next() })

	for _, path := range []string{"/api/auth/login-by-email", "/api/auth/register-by-email"} {
		if w := postJSON(t, r, path, map[string]string{"email": "a@b.com", "password": "123456"}); w.Code != http.StatusNotFound {
			t.Errorf("%s is still routed (status %d); the email path must be removed", path, w.Code)
		}
	}
	for _, path := range []string{
		"/api/auth/captcha",
		"/api/auth/send-sms-code",
		"/api/auth/register",
		"/api/auth/login",
		"/api/auth/login-by-phone",
	} {
		found := false
		for _, route := range r.Routes() {
			if route.Path == path {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not registered", path)
		}
	}
}

// ========== GetMe ==========

func TestGetMe_ReturnsUser(t *testing.T) {
	h := NewHandler(&mockAuthService{})
	r := setupTestRouter(h)
	// Simulate an authenticated user.
	r.GET("/me", func(c *gin.Context) {
		c.Set("user_id", int64(42))
		c.Next()
	}, h.GetMe)

	req := httptest.NewRequest("GET", "/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ========== Profile ==========

// The account payload is published to every client and cached in localStorage, so its shape is asserted
// rather than assumed: the profile is the phone number and nothing else (see domain.UserInfo).
func TestUserInfoPublishesOnlyThePhone(t *testing.T) {
	body, err := json.Marshal(domain.UserInfo{ID: 7, Phone: strPtr("13800138000")})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if got, want := string(body), `{"id":7,"phone":"13800138000"}`; got != want {
		t.Errorf("user payload = %s, want %s", got, want)
	}
}

// PATCH /me is removed, not merely unused. It accepted a name and an email - fields the account no longer
// has - so it would now answer 200 to a request that changes nothing, which is worse than a 404.
func TestRegisterRoutes_ProfileUpdateIsGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api")
	NewHandler(&mockAuthService{}).RegisterRoutes(v1, func(c *gin.Context) { c.Next() })

	req := httptest.NewRequest("PATCH", "/api/me", bytes.NewReader([]byte(`{"name":"paloma"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("PATCH /api/me answered %d; the profile-update route must be removed", w.Code)
	}
}
