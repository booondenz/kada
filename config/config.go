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
	// UploadDir is where uploaded files are stored. It has to be a directory this process may write to,
	// and one that survives a deploy - the files are the content of file links, not a cache.
	UploadDir string

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

	// WeChat
	WechatAppID     string
	WechatAppSecret string

	// AdminSecret is the password for the operator routes (ADMIN_SECRET).
	//
	// It is a shared secret rather than an account because there are no accounts: the site is free and every
	// visitor has the same kind of session, so the operator proves who they are with something only the
	// deployment knows. Empty means nobody - the operator routes then refuse everybody, which is the safe
	// direction for a missing setting.
	AdminSecret string
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
		UploadDir:   getEnv("UPLOAD_DIR", "./data/uploads"),

		AIDeepSeekBaseURL: getEnv("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		AIDeepSeekAPIKey:  getEnv("DEEPSEEK_API_KEY", ""),
		AIDeepSeekModel:   getEnv("AI_CHAT_MODEL", "deepseek-flash"),
		AIMaxTokens:       getEnvInt("AI_MAX_TOKENS", 8192),
		AutoMigrate:       getEnvBool("DB_AUTO_MIGRATE", true),
		WechatAppID:       getEnv("WECHAT_APP_ID", ""),
		WechatAppSecret:   getEnv("WECHAT_APP_SECRET", ""),
		AdminSecret:       getEnv("ADMIN_SECRET", ""),
	}
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
