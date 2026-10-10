package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/domain/entity"
	"github.com/chun/kada-backend/internal/infra/captcha"
	"github.com/chun/kada-backend/internal/infra/sms"
	"github.com/chun/kada-backend/internal/middleware"
)

// SMSSender is the SMS sending interface
type SMSSender interface {
	SendVerificationCode(phone string) (code string, err error)
	CheckVerificationCode(phone, code string) (bool, error)
}

// Sending an SMS costs money and can be used to harass whoever owns the number, so three independent
// quotas have to hold before a message leaves the building:
//
//	per phone, 60s   - stops a single number being bombed and makes each attempt cost the attacker time;
//	per phone, 10/day - bounds the damage to one victim to ten messages a day;
//	per IP, 10/hour and 30/day - bounds a script that walks through a list of victim numbers, which the
//	                  per-phone quota alone would never notice (each number is used once).
//
// The per-IP figures are deliberately looser than the per-phone ones because a campus or office NAT puts
// many legitimate users behind one address; they are there to stop a bulk run, not to police a household.
const (
	smsPhoneCooldown = 60 * time.Second
	smsPhoneDailyMax = 10
	smsIPHourlyMax   = 10
	smsIPDailyMax    = 30

	// smsProviderThrottleWait is how long a user is told to wait when the provider itself refuses a send.
	// Aliyun's send interval defaults to 60 seconds, which is also this service's per-phone cooldown, so
	// both sides agree on the window that has to be sat out.
	smsProviderThrottleWait = 60
)

// Graphical challenge lifetime and tolerance. Five minutes is long enough to read a distorted code and
// type it, and short enough that a harvested id is useless. Five attempts bounds an automated solver.
const (
	captchaTTL         = 5 * time.Minute
	captchaMaxAttempts = 5
)

type AuthService struct {
	db        *gorm.DB
	jwtSecret string
	jwtExpire time.Duration
	sms       SMSSender // SMS sender
}

func NewAuthService(db *gorm.DB, jwtSecret, jwtExpire string, sms SMSSender) *AuthService {
	d, _ := time.ParseDuration(jwtExpire)
	return &AuthService{db: db, jwtSecret: jwtSecret, jwtExpire: d, sms: sms}
}

// phonePattern matches mainland China mobile numbers: leading 1 + 3-9 + 9 digits
var phonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

// GenerateCaptcha issues a graphical challenge for the client to solve before it may request an SMS.
//
// Expired rows are purged here rather than by a background job: this is the only place that creates them,
// so the table cannot grow without bound and the deployment needs no scheduler.
func (s *AuthService) GenerateCaptcha(ctx context.Context, ip string) (*domain.CaptchaResponse, error) {
	code, err := captcha.Code()
	if err != nil {
		return nil, errors.New("failed to generate a verification code, please try again later")
	}

	id, err := randomID()
	if err != nil {
		return nil, errors.New("failed to generate a verification code, please try again later")
	}

	row := entity.LoginCaptcha{
		ID:        id,
		CodeHash:  sha256Hex(captcha.Normalize(code)),
		IP:        stringPtr(ip),
		ExpiresAt: time.Now().Add(captchaTTL),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		log.Printf("store captcha failed: %v", err)
		return nil, errors.New("failed to generate a verification code, please try again later")
	}

	// Housekeeping: anything that expired more than an hour ago can never be answered.
	if err := s.db.WithContext(ctx).Exec(
		`DELETE FROM login_captchas WHERE expires_at < NOW() - INTERVAL '1 hour'`).Error; err != nil {
		log.Printf("purge expired captchas failed: %v", err)
	}

	return &domain.CaptchaResponse{ID: id, Image: captcha.DataURI(code)}, nil
}

// consumeCaptcha validates an answer and burns it in a single statement.
//
// The UPDATE ... WHERE used = FALSE is the whole point: two concurrent requests carrying the same solved
// challenge cannot both succeed, so one solved captcha cannot be reused to fan out SMS sends.
func (s *AuthService) consumeCaptcha(ctx context.Context, id, answer string) error {
	if id == "" || answer == "" {
		return errors.New("the graphical verification code is required")
	}

	var consumed string
	err := s.db.WithContext(ctx).Raw(`
		UPDATE login_captchas SET used = TRUE
		WHERE id = ?
		  AND code_hash = ?
		  AND used = FALSE
		  AND expires_at > NOW()
		  AND attempts < ?
		RETURNING id
	`, id, sha256Hex(captcha.Normalize(answer)), captchaMaxAttempts).Scan(&consumed).Error
	if err == nil && consumed != "" {
		return nil
	}
	if err != nil {
		log.Printf("consume captcha %s failed: %v", id, err)
	}

	// Count the failure against every still-usable challenge for this id, so a solver cannot simply retry
	// the same challenge forever.
	_ = s.db.WithContext(ctx).Exec(`
		UPDATE login_captchas SET attempts = attempts + 1
		WHERE id = ? AND used = FALSE AND expires_at > NOW()
	`, id).Error

	return errors.New("the graphical verification code is incorrect or has expired")
}

// SendSMSCode sends an SMS verification code.
//
// The order of the checks is deliberate and is the order in which each one can reject the request most
// cheaply: the captcha (fails a script outright and costs nothing), the phone quotas, then the IP quotas,
// and only then the provider call, which is the only step that costs money.
func (s *AuthService) SendSMSCode(ctx context.Context, phone, ip, captchaID, captchaAnswer string) error {
	if !phonePattern.MatchString(phone) {
		return errors.New("invalid phone number format")
	}

	if err := s.consumeCaptcha(ctx, captchaID, captchaAnswer); err != nil {
		return err
	}

	// 60-second cooldown per phone number: prevents SMS bombing of a single number
	var recent int64
	if err := s.db.WithContext(ctx).Model(&entity.SMSVerificationCode{}).
		Where("phone = ? AND created_at > NOW() - INTERVAL '60 seconds'", phone).
		Count(&recent).Error; err == nil && recent > 0 {
		return &domain.RateLimitError{
			Message:           "too many requests, please try again in 60 seconds",
			RetryAfterSeconds: int(smsPhoneCooldown.Seconds()),
		}
	}

	// daily cap of 10 per phone number: prevents bulk bombing and SMS cost loss
	var daily int64
	if err := s.db.WithContext(ctx).Model(&entity.SMSVerificationCode{}).
		Where("phone = ? AND created_at > NOW() - INTERVAL '24 hours'", phone).
		Count(&daily).Error; err == nil && daily >= smsPhoneDailyMax {
		return &domain.RateLimitError{Message: "this phone number has reached its daily send limit, please try again tomorrow"}
	}

	// Per-IP quotas. They are skipped when the address is unknown: recording every unnamed caller as
	// 0.0.0.0 and then counting them together would lock out the whole world the moment one script ran.
	if ip != "" && ip != "0.0.0.0" {
		var fromIPHour int64
		if err := s.db.WithContext(ctx).Model(&entity.SMSVerificationCode{}).
			Where("ip = ? AND created_at > NOW() - INTERVAL '1 hour'", ip).
			Count(&fromIPHour).Error; err == nil && fromIPHour >= smsIPHourlyMax {
			return &domain.RateLimitError{
				Message:           "too many verification codes requested from this network, please try again later",
				RetryAfterSeconds: 3600,
			}
		}

		var fromIPDay int64
		if err := s.db.WithContext(ctx).Model(&entity.SMSVerificationCode{}).
			Where("ip = ? AND created_at > NOW() - INTERVAL '24 hours'", ip).
			Count(&fromIPDay).Error; err == nil && fromIPDay >= smsIPDailyMax {
			return &domain.RateLimitError{Message: "this network has reached its daily limit for verification codes, please try again tomorrow"}
		}
	}

	var code string
	var err error

	if s.sms != nil {
		code, err = s.sms.SendVerificationCode(phone)
		if err != nil {
			log.Printf("send sms code to %s failed: %v", phone, err)
			return smsSendError(err)
		}
	} else {
		code = generateSMSCode()
		// security: the plaintext code is only printed outside release mode; production logs never contain the code
		if os.Getenv("GIN_MODE") != "release" {
			fmt.Printf("[DEV] Phone: %s, Code: %s\n", phone, code)
		}
	}

	// store the code hash in the database (valid for 5 minutes): no plaintext is persisted, so a database leak cannot be replayed directly
	if err := s.db.WithContext(ctx).Exec(`
		INSERT INTO sms_codes (phone, code_hash, ip, expires_at)
		VALUES (?, ?, ?, ?)
	`, phone, sha256Hex(code), stringPtr(ip), time.Now().Add(5*time.Minute)).Error; err != nil {
		log.Printf("store sms code failed: %v", err)
		return errors.New("failed to store verification code, please try again later")
	}

	return nil
}

// smsSendError turns a sender failure into the error the API should report.
//
// The provider's own rate limit is a wait, not a mistake, and its error code is not something a user can
// act on: answering 400 with "biz.FREQUENCY" buried in the message is how an English provider code ended up
// on a Chinese sign-in form. A throttle becomes a quota instead, which the handler already answers as a 429
// with a retry-after the client can count down.
//
// Every other failure keeps its cause in the chain. The provider code is what identifies an unapproved
// signature or template, and without it a failed sign-up is undebuggable; the user still only ever sees the
// flat "failed to send SMS" text unless the service runs outside release mode.
func smsSendError(err error) error {
	if errors.Is(err, sms.ErrProviderThrottled) {
		return &domain.RateLimitError{
			Message:           "verification codes are being requested too often, please try again in 60 seconds",
			RetryAfterSeconds: smsProviderThrottleWait,
		}
	}
	return fmt.Errorf("failed to send SMS: %w", err)
}

// LoginByPhone logs in with phone number + verification code.
//
// It no longer creates the account: registering does, and it asks for a password, so that signing in never
// has to send a message again. What is left here is the way back in for an account that has no password
// yet - every account that existed before this flow did - and the way for such an account to be reached at
// all, since it cannot be signed into with a password it does not have.
func (s *AuthService) LoginByPhone(ctx context.Context, phone, code string) (*domain.AuthResponse, error) {
	if !phonePattern.MatchString(phone) {
		return nil, errors.New("invalid phone number format")
	}

	if err := s.consumeSMSCode(ctx, phone, code); err != nil {
		return nil, err
	}

	user, userErr := s.userByPhone(ctx, phone)
	if userErr != nil {
		if !errors.Is(userErr, gorm.ErrRecordNotFound) {
			// A read error that is not "no such row" (connection loss, ...) must not be reported as a missing
			// account, which is a message the user can act on and would act on wrongly.
			log.Printf("lookup user by phone %s failed: %v", phone, userErr)
			return nil, errors.New("login failed, please try again later")
		}
		return nil, errors.New("this phone number has no account yet, please register")
	}

	return s.startSession(ctx, toUserInfo(*user))
}

// Register creates an account, or claims one that has no password yet.
//
// The second half is what makes this deployable. Every account that exists today was created by the SMS
// login, which never wrote a password_hash, so "register" is also how those accounts get one - and the code
// proves possession of the number, exactly as logging in by SMS did, so nothing is granted that the old
// flow did not already grant. An account that *does* have a password is refused rather than overwritten:
// otherwise receiving one SMS would be enough to take an account over.
func (s *AuthService) Register(ctx context.Context, phone, code, password string) (*domain.AuthResponse, error) {
	if !phonePattern.MatchString(phone) {
		return nil, errors.New("invalid phone number format")
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	if err := s.consumeSMSCode(ctx, phone, code); err != nil {
		return nil, err
	}

	hash := hashPassword(password)
	if hash == "" {
		// hashPassword degrades to "" when bcrypt itself fails. For a link password that means "no password
		// required"; here it would mean an account whose password can never match, created by a request that
		// reported success, so it is refused instead.
		return nil, errors.New("failed to set the password, please try again")
	}

	row, err := s.userByPhone(ctx, phone)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		row = &entity.User{Phone: &phone, PasswordHash: &hash}
		if createErr := s.db.WithContext(ctx).Create(row).Error; createErr != nil {
			log.Printf("create user by phone %s failed: %v", phone, createErr)
			return nil, errors.New("registration failed, please try again later")
		}

	case err != nil:
		log.Printf("lookup user by phone %s failed: %v", phone, err)
		return nil, errors.New("registration failed, please try again later")

	case row.PasswordHash != nil && *row.PasswordHash != "":
		return nil, domain.ErrAlreadyRegistered

	default:
		if updateErr := s.db.WithContext(ctx).Model(&entity.User{}).
			Where("id = ?", row.ID).
			Update("password_hash", hash).Error; updateErr != nil {
			log.Printf("set the password of user %d failed: %v", row.ID, updateErr)
			return nil, errors.New("registration failed, please try again later")
		}
		row.PasswordHash = &hash
	}

	return s.startSession(ctx, toUserInfo(*row))
}

// LoginWithPassword signs in with the password chosen at registration.
//
// Every failure returns the same error, and a number with no account is still verified against a throwaway
// hash: without that, the endpoint answers "does this number have an account here" faster than it answers
// "is this the right password", and the timing is the leak.
func (s *AuthService) LoginWithPassword(ctx context.Context, phone, password string) (*domain.AuthResponse, error) {
	if !phonePattern.MatchString(phone) {
		return nil, errors.New("invalid phone number format")
	}

	row, err := s.userByPhone(ctx, phone)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// The same work as a real verification, against a hash nobody knows the password to.
		_ = checkPasswordHash(password, dummyPasswordHash)
		return nil, domain.ErrInvalidCredentials

	case err != nil:
		log.Printf("lookup user by phone %s failed: %v", phone, err)
		return nil, errors.New("login failed, please try again later")

	case row.PasswordHash == nil || *row.PasswordHash == "":
		// An account from before passwords, or one created by the SMS login. Same answer as a wrong password:
		// the caller cannot be told which, and the page offers the code path to everybody anyway.
		_ = checkPasswordHash(password, dummyPasswordHash)
		return nil, domain.ErrInvalidCredentials

	case !checkPasswordHash(password, *row.PasswordHash):
		return nil, domain.ErrInvalidCredentials
	}

	// A hash written before bcrypt was in use is upgraded the first time the password verifies. Leaving it
	// alone because that is how it was stored would keep the older accounts on an unsalted digest for good,
	// and the plaintext is only in hand at this one moment.
	if isLegacyHash(*row.PasswordHash) {
		if upgraded := hashPassword(password); upgraded != "" {
			if err := s.db.WithContext(ctx).Model(&entity.User{}).
				Where("id = ?", row.ID).
				Update("password_hash", upgraded).Error; err != nil {
				log.Printf("upgrade the password hash of user %d failed: %v", row.ID, err)
			}
		}
	}

	return s.startSession(ctx, toUserInfo(*row))
}

// consumeSMSCode validates a verification code and burns it, in one statement.
//
// The UPDATE ... RETURNING is what makes the code single-use even under concurrent requests. The failure
// path counts the attempt against every pending code for the number: the row used to be located *by* the
// code, so a wrong code matched no row and the attempts counter never moved.
func (s *AuthService) consumeSMSCode(ctx context.Context, phone, code string) error {
	var codeID int64
	err := s.db.WithContext(ctx).Raw(`
		UPDATE sms_codes SET used = TRUE
		WHERE id = (
			SELECT id FROM sms_codes
			WHERE phone = ? AND code_hash = ? AND used = FALSE
			  AND expires_at > NOW() AND attempts < 5
			ORDER BY id
			LIMIT 1
		)
		RETURNING id
	`, phone, sha256Hex(code)).Scan(&codeID).Error
	if err != nil || codeID == 0 {
		_ = s.db.WithContext(ctx).Exec(`
			UPDATE sms_codes SET attempts = attempts + 1
			WHERE phone = ? AND used = FALSE AND expires_at > NOW()
		`, phone).Error
		return errors.New("invalid or expired verification code")
	}
	return nil
}

// startSession records the sign-in and issues the token.
//
// Both ways in end here, so "last login" and the token cannot drift apart between them - and a missing
// timestamp is not worth refusing a sign-in that has already been proved.
func (s *AuthService) startSession(ctx context.Context, user *domain.UserInfo) (*domain.AuthResponse, error) {
	if err := s.db.WithContext(ctx).Model(&entity.User{}).
		Where("id = ?", user.ID).
		Update("last_login_at", time.Now()).Error; err != nil {
		log.Printf("update last login for user %d failed: %v", user.ID, err)
	}

	token, err := s.generateToken(*user)
	if err != nil {
		return nil, err
	}

	return &domain.AuthResponse{Token: token, User: *user}, nil
}

// Password policy: a length floor and nothing else. Character-class rules push people towards "Password1!"
// and a note on the desk, and the ceiling is bcrypt's own limit - it reads only the first 72 bytes, so a
// longer password is refused rather than quietly cut down to a different one.
const (
	passwordMinLength = 8
	passwordMaxBytes  = 72
)

// dummyPasswordHash is what a sign-in for a number with no account is verified against, so that "no such
// account" and "wrong password" take the same time. It is a bcrypt hash of a string nobody is told, and it
// is deliberately never stored on an account.
//
// #nosec G101 -- not a credential: a decoy that exists to be compared against and never to match.
const dummyPasswordHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// validatePassword rejects a password that cannot be the credential it is meant to be.
func validatePassword(password string) error {
	if len([]rune(password)) < passwordMinLength {
		return fmt.Errorf("the password must be at least %d characters", passwordMinLength)
	}
	if len(password) > passwordMaxBytes {
		return fmt.Errorf("the password must be at most %d bytes", passwordMaxBytes)
	}
	return nil
}

// LoginByEmail and RegisterByEmail used to live here. They are gone: signing in happens by phone number
// only (see entity.User), and nothing writes an email onto an account any more either - the profile is the
// phone number, so there is no second field for a caller to edit.

// GetUserByID gets user info
func (s *AuthService) GetUserByID(ctx context.Context, userID int64) (*domain.UserInfo, error) {
	var row entity.User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&row).Error; err != nil {
		return nil, errors.New("user not found")
	}
	return toUserInfo(row), nil
}

// userByPhone returns the account row for a phone number.
//
// It returns the row rather than the API model because the password sign-in needs the hash, and the caller
// has to distinguish "no such user" (gorm.ErrRecordNotFound) from a real read error.
func (s *AuthService) userByPhone(ctx context.Context, phone string) (*entity.User, error) {
	var row entity.User
	if err := s.db.WithContext(ctx).Where("phone = ?", phone).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// toUserInfo maps the persistence model onto the API model.
//
// The mapping is deliberately thin: UserInfo carries the phone number, so password_hash and the legacy
// email/wechat columns have nowhere to go even by accident.
func toUserInfo(row entity.User) *domain.UserInfo {
	return &domain.UserInfo{ID: row.ID, Phone: row.Phone}
}

// generateToken generates a JWT
func (s *AuthService) generateToken(user domain.UserInfo) (string, error) {
	claims := middleware.Claims{
		UserID: user.ID,
		Phone:  user.Phone,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "kada",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

// generateSMSCode generates a 6-digit numeric verification code
func generateSMSCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return fmt.Sprintf("%06d", n.Int64())
}

// randomID returns 32 hex characters from crypto/rand: the opaque handle for a captcha.
//
// It is not a UUID because nothing here needs the version/variant bits; what matters is that it cannot be
// guessed from another challenge, which a counter or a timestamp would allow.
func randomID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", buf), nil
}

// stringPtr returns nil for an empty string, so "no address" is stored as SQL NULL rather than as ”.
// An empty string would otherwise be a value a COUNT query matches, quietly lumping every unnamed caller
// into one quota bucket.
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
