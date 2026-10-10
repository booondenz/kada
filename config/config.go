package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port        string
	DatabaseURL string
	RedisURL    string
	JWTSecret   string
	JWTExpires  string
	BaseURL     string
	FrontendURL string

	// The assistant's own settings. It runs in this process, so the model keys are read here; there is no
	// second service, no internal secret and no second database to point at.
	AIDeepSeekBaseURL string
	AIDeepSeekAPIKey  string
	// AIDeepSeekModel keeps the name the Python service used. The name is a product decision, not part of
	// the port, so it is unchanged here even though it is an unusual one for api.deepseek.com.
	AIDeepSeekModel string
	AIMaxTokens     int

	// AutoMigrate controls whether the process is allowed to create/update the schema at startup.
	// Disable it (DB_AUTO_MIGRATE=false) once the schema is managed out of band.
	AutoMigrate bool

	// SMS service
	SMSAccessKeyID     string
	SMSAccessKeySecret string
	SMSSignName        string
	SMSTemplateCode    string

	// WeChat
	WechatAppID     string
	WechatAppSecret string

	// Kafka (click event stream; empty = disabled)
	KafkaBrokers string
	KafkaTopic   string
}

func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://kada:kada123@localhost:5432/kada?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:   getEnv("JWT_SECRET", "kada-dev-secret-change-in-production"),
		JWTExpires:  getEnv("JWT_EXPIRES_IN", "720h"),
		BaseURL:     getEnv("API_BASE_URL", "https://kada.click"),
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:3000"),

		AIDeepSeekBaseURL: getEnv("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		AIDeepSeekAPIKey:  getEnv("DEEPSEEK_API_KEY", ""),
		AIDeepSeekModel:   getEnv("AI_CHAT_MODEL", "deepseek-flash"),
		AIMaxTokens:       getEnvInt("AI_MAX_TOKENS", 8192),
		AutoMigrate:       getEnvBool("DB_AUTO_MIGRATE", true),
		// SMS_SIGN_NAME has no default. A placeholder signature such as "kada" is not a value this account
		// holds, and it turned "nobody configured SMS" into an Aliyun rejection that reads like a broken
		// account: the startup guard in sms.NewAliyunSender never fired, and the failure only appeared when
		// a real user tried to sign in. Empty means "not configured", which is what the startup log says.
		SMSAccessKeyID:     getEnv("SMS_ACCESS_KEY_ID", ""),
		SMSAccessKeySecret: getEnv("SMS_ACCESS_KEY_SECRET", ""),
		SMSSignName:        getEnv("SMS_SIGN_NAME", ""),
		SMSTemplateCode:    getEnv("SMS_TEMPLATE_CODE", ""),
		WechatAppID:        getEnv("WECHAT_APP_ID", ""),
		WechatAppSecret:    getEnv("WECHAT_APP_SECRET", ""),
		KafkaBrokers:       getEnv("KAFKA_BROKERS", ""),
		KafkaTopic:         getEnv("KAFKA_TOPIC", "clicks"),
	}
}

// Brokers splits the comma-separated broker list, trimming whitespace and empty entries
func (c *Config) Brokers() []string {
	return SplitBrokers(c.KafkaBrokers)
}

// SplitBrokers splits the comma-separated broker list, trimming whitespace and empty entries (shared by server and worker)
func SplitBrokers(raw string) []string {
	var out []string
	for _, b := range strings.Split(raw, ",") {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return out
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvBool parses a boolean environment variable; anything unparsable (including an empty value)
// falls back to the default rather than silently disabling the feature.
func getEnvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// getEnvInt parses an integer environment variable; an unparsable value falls back to the default, the
// same way getEnvBool does, rather than turning a typo into a zero.
func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// weakJWTSecrets lists known weak default secrets: startup fails outright in release mode
var weakJWTSecrets = []string{
	"",
	"kada-dev-secret-change-in-production",
	"changeme",
	"secret",
	"jwt-secret",
}

// IsWeakJWTSecret reports whether the secret is a known weak one
func IsWeakJWTSecret(s string) bool {
	for _, w := range weakJWTSecrets {
		if s == w {
			return true
		}
	}
	return false
}

// placeholderSMSValues are the sample values shipped in .env.example. Copying them verbatim yields a
// non-empty key pair that still fails every Aliyun call with a signature/credential error, which looks
// like a code bug logged deep inside a request handler. Detecting them turns it into a startup warning.
var placeholderSMSValues = []string{
	"your_access_key_id",
	"your_access_key_secret",
	"change-me",
	"changeme",
}

// SMSCredentialsConfigured reports whether SMS_ACCESS_KEY_ID/SECRET hold real values.
func (c *Config) SMSCredentialsConfigured() bool {
	if c.SMSAccessKeyID == "" || c.SMSAccessKeySecret == "" {
		return false
	}
	for _, p := range placeholderSMSValues {
		if strings.EqualFold(c.SMSAccessKeyID, p) || strings.EqualFold(c.SMSAccessKeySecret, p) {
			return false
		}
	}
	return true
}
