package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/booondenz/kada/config"
	"github.com/booondenz/kada/internal/assistant"
	adminHandler "github.com/booondenz/kada/internal/handler/admin"
	aiHandler "github.com/booondenz/kada/internal/handler/ai"
	analyticsHandler "github.com/booondenz/kada/internal/handler/analytics"
	authHandler "github.com/booondenz/kada/internal/handler/auth"
	domainHandler "github.com/booondenz/kada/internal/handler/domain"
	folderHandler "github.com/booondenz/kada/internal/handler/folder"
	linkHandler "github.com/booondenz/kada/internal/handler/link"
	redirectHandler "github.com/booondenz/kada/internal/handler/redirect"
	reportHandler "github.com/booondenz/kada/internal/handler/report"
	tagHandler "github.com/booondenz/kada/internal/handler/tag"
	tokenHandler "github.com/booondenz/kada/internal/handler/token"
	utmHandler "github.com/booondenz/kada/internal/handler/utm"
	"github.com/booondenz/kada/internal/infra"
	"github.com/booondenz/kada/internal/infra/blocklist"
	"github.com/booondenz/kada/internal/infra/sms"
	"github.com/booondenz/kada/internal/middleware"
	"github.com/booondenz/kada/internal/mq"
	"github.com/booondenz/kada/internal/service"
)

func main() {
	// Load the .env file
	_ = godotenv.Load()

	// Load configuration
	cfg := config.Load()

	// Security: release mode forbids the default/weak JWT secret, otherwise anyone could forge login tokens
	if os.Getenv("GIN_MODE") == "release" && config.IsWeakJWTSecret(cfg.JWTSecret) {
		log.Fatal("refusing to use the default JWT_SECRET in production; set a strong random secret (e.g. openssl rand -hex 32)")
	}

	// Connect to the database
	db, err := infra.NewDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer infra.CloseDB(db)

	// Apply the schema (GORM AutoMigrate owns the schema now; see internal/domain/entity).
	// In production DB_AUTO_MIGRATE is normally false and the schema is applied by cmd/migrate, so a
	// plain restart can never change the database.
	if cfg.AutoMigrate {
		if migrateErr := infra.Migrate(db); migrateErr != nil {
			log.Fatalf("Failed to migrate database: %v", migrateErr)
		}
	} else {
		log.Println("DB_AUTO_MIGRATE=false: skipping schema migration (run cmd/migrate to apply it)")
	}

	// Connect to Redis: keep the client even on initial ping failure (go-redis reconnects automatically).
	// Rate limiting fails open until Redis recovers, then works again with no process restart.
	redisClient, err := infra.NewRedis(cfg.RedisURL)
	if err != nil {
		log.Printf("Redis temporarily unavailable (rate limiting will fail open until it recovers): %v", err)
	}
	if redisClient != nil {
		defer infra.CloseRedis(redisClient)
	}

	// Initialize the Aliyun SMS verification service.
	//
	// Phone + SMS code is how a guest account is claimed, and the only way into an account that has one, so a
	// sender that cannot start is not cosmetic: without it every account stays a guest, and a guest's links
	// live in one browser. The process still starts (an API that refuses to boot is a worse outage than one
	// that cannot send messages) but it says so in the log, at the top, every time.
	var smsSender service.SMSSender
	if cfg.SMSCredentialsConfigured() {
		sender, senderErr := sms.NewAliyunSender(cfg.SMSAccessKeyID, cfg.SMSAccessKeySecret, cfg.SMSSignName, cfg.SMSTemplateCode)
		if senderErr != nil {
			// Not fatal: every other feature still works. But no account can be claimed with a phone number,
			// and the reason is printed now rather than deferred to a request that would only report a
			// provider error.
			log.Printf("SMS service disabled, so no account can be claimed with a phone number: %v", senderErr)
		} else {
			// Assigned only on success. The constructor returns (nil, error), and a nil *AliyunSender stored
			// in this interface is not nil: the service's own `sms != nil` guard would pass, and every send
			// would call a method on a nil receiver instead of using the development fallback that prints
			// the code to the log.
			smsSender = sender
		}
	} else {
		// Distinguish "not configured" from "configured with the .env.example placeholders": both end in the
		// same warning, but the second one is the common cause of a send failing with a provider error.
		if cfg.SMSAccessKeyID != "" || cfg.SMSAccessKeySecret != "" {
			log.Println("SMS_ACCESS_KEY_ID/SMS_ACCESS_KEY_SECRET still hold the .env.example placeholder values; " +
				"no account can be claimed with a phone number until real Aliyun credentials are set")
		} else {
			log.Println("SMS not configured, so no account can be claimed with a phone number. Verification codes " +
				"are printed to this log instead (development only); set SMS_ACCESS_KEY_ID/SMS_ACCESS_KEY_SECRET/" +
				"SMS_SIGN_NAME/SMS_TEMPLATE_CODE to enable real sending")
		}
	}

	// Initialize the cache service (if Redis is available). The variable is the interface rather than
	// *CacheService on purpose: a nil *CacheService stored in an interface is not nil, so every
	// `cache != nil` guard in the service would pass and the first cache call would dereference it.
	var cacheSvc service.LinkCache
	if redisClient != nil {
		cacheSvc = service.NewCacheService(redisClient)
	}

	// Kafka click event publisher (returns nil when there is no broker = disabled).
	//
	// The variable is the interface, for the same reason as the cache above - and this one is not a
	// hypothetical. A nil *KafkaClickPublisher stored in a non-nil interface passed the `kafka != nil` guard
	// in the service, so every click called a method on a nil receiver, panicked on the goroutine the
	// redirect handler spawns for it (which Gin's recovery does not cover), and took the whole API down:
	// systemd restarted it, and nginx answered 502 for whatever arrived in the seconds between. It also meant
	// the direct-write fallback below never ran on a host without Kafka - the clicks were lost, not degraded.
	var kafkaPub mq.ClickPublisher
	if pub := mq.NewKafkaClickPublisher(cfg.Brokers(), cfg.KafkaTopic); pub != nil {
		defer pub.Close()
		kafkaPub = pub
	}

	// Initialize the service layer
	authSvc := service.NewAuthService(db, cfg.JWTSecret, cfg.JWTExpires, smsSender)

	// The blocked-host list: rows an operator edits, held in memory because the redirect path asks the
	// question on every request. Failing to load it is a warning and not a refused start - links stay
	// served, and the reload below picks the table up as soon as the database answers. It never blocks
	// nothing silently: the list is empty until a load succeeds, and an empty list is the behavior this
	// service had before the table existed.
	blockList := blocklist.New(db)
	if loadErr := blockList.Reload(context.Background()); loadErr != nil {
		log.Printf("load blocked hosts failed, the list stays empty until the next reload: %v", loadErr)
	}
	go blockList.Run(context.Background(), 30*time.Second)

	linkSvc := service.NewLinkService(db, cfg.BaseURL, cacheSvc, kafkaPub, service.NewClickStore(db), blockList)
	domainSvc := service.NewDomainService(db)
	folderSvc := service.NewFolderService(db)
	tagSvc := service.NewTagService(db)
	utmSvc := service.NewUTMTemplateService(db)
	tokenSvc := service.NewAPITokenService(db)
	analyticsSvc := service.NewAnalyticsService(db)
	reportSvc := service.NewReportService(db)

	// Initialize the handler layer
	authH := authHandler.NewHandler(authSvc)
	linkH := linkHandler.NewHandler(linkSvc)
	redirectH := redirectHandler.NewHandler(linkSvc)
	domainH := domainHandler.NewHandler(domainSvc)
	folderH := folderHandler.NewHandler(folderSvc)
	tagH := tagHandler.NewHandler(tagSvc)
	utmH := utmHandler.NewHandler(utmSvc)
	tokenH := tokenHandler.NewHandler(tokenSvc)
	analyticsH := analyticsHandler.NewHandler(db, analyticsSvc)
	adminH := adminHandler.NewHandler(linkSvc, reportSvc)
	reportH := reportHandler.NewHandler(reportSvc)

	// The assistant: it used to be a separate Python service that this binary reverse-proxied to, and it
	// runs in this process now. Both halves are optional on purpose - a missing key is a warning rather
	// than a refused start, so /api/ai/chat answers with a configuration message instead of the whole API
	// being down.
	chatModel, err := assistant.NewChatModel(context.Background(), assistant.ChatModelConfig{
		APIKey:    cfg.AIDeepSeekAPIKey,
		BaseURL:   cfg.AIDeepSeekBaseURL,
		Model:     cfg.AIDeepSeekModel,
		MaxTokens: cfg.AIMaxTokens,
	})
	if err != nil {
		log.Printf("The assistant will report a configuration problem: %v", err)
	}

	// Building the agent can only fail on a programming error (a tool whose schema cannot be inferred), so
	// it is the one assistant failure that stops the process: there would be nothing to answer with.
	assistantService, err := assistant.NewAssistant(
		context.Background(),
		chatModel,
		assistant.NewKada(linkSvc, analyticsSvc),
		assistant.NewConversations(db),
	)
	if err != nil {
		log.Fatalf("Failed to initialize the assistant: %v", err)
	}
	aiH := aiHandler.NewHandler(assistantService)

	// JWT + API Token middleware
	authMW := middleware.JWTAuth(cfg.JWTSecret, tokenSvc)

	// Rate limiting middleware
	var rateLimiter *middleware.RateLimiter
	if redisClient != nil {
		rateLimiter = middleware.NewRateLimiter(redisClient)
	}

	// Create the Gin engine
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()

	// Security: trust no X-Forwarded-For sent by any proxy (clients can forge it; it has been used to bypass rate limiting).
	// The real client IP comes from the X-Real-IP header rewritten by nginx (see middleware.RealIP).
	_ = r.SetTrustedProxies(nil)

	// Global rate limiting (if Redis is available)
	if rateLimiter != nil {
		r.Use(rateLimiter.Normal())
	}

	// Health check
	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "kada-api",
			"version": "0.2.0",
		})
	})

	// Metrics, in the Prometheus text format. It carries the redirect path's own numbers - what the
	// handler answered and how long it took, what the short-code cache did, and what happened to each
	// click - plus the Go runtime's.
	//
	// It is deliberately not exposed to the internet. nginx proxies /api/, /r/ and /assets/ and serves
	// everything else from the frontend build, so a request for /metrics from outside gets index.html;
	// reaching it means being on the host or holding a tunnel. Making it public is a decision with a
	// consequence - these series describe traffic and cache behavior - so it wants a token or an
	// allowlist in front of it, not a wider bind.
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Short-link redirection (public endpoint, high-traffic rate limit), and the guide page's own reports
	// under a tighter one of their own: see RegisterRoutes for why the two differ.
	var redirectMW, actionMW gin.HandlerFunc
	if rateLimiter != nil {
		redirectMW = rateLimiter.Redirect()
		actionMW = rateLimiter.Action()
	}
	redirectH.RegisterRoutes(r, redirectMW, actionMW)

	// API v1 route group
	v1 := r.Group("/api")
	{
		// Auth routes (strict rate limit, and a tighter one on creating an account)
		var strictMW, guestMW gin.HandlerFunc
		if rateLimiter != nil {
			strictMW = rateLimiter.Strict()
			guestMW = rateLimiter.Guest()
		}
		authH.RegisterRoutes(v1, authMW, strictMW, guestMW)
		linkH.RegisterRoutes(v1, authMW)
		// Always mounted, never conditionally: RequireAdmin refuses everybody when ADMIN_PHONES is empty,
		// and a middleware left out of the chain would allow everybody.
		adminH.RegisterRoutes(v1, authMW, middleware.RequireAdmin(cfg.AdminPhones))
		reportH.RegisterRoutes(v1, strictMW)
		domainH.RegisterRoutes(v1, authMW)
		folderH.RegisterRoutes(v1, authMW)
		tagH.RegisterRoutes(v1, authMW)
		utmH.RegisterRoutes(v1, authMW)
		tokenH.RegisterRoutes(v1, authMW)
		analyticsH.RegisterRoutes(v1, authMW)
		aiH.RegisterRoutes(v1, authMW)
	}

	// Start the server (http.Server for graceful shutdown)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second, // gosec G112: prevent Slowloris slow-header attacks
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("Kada API server starting on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Graceful shutdown: wait for SIGINT/SIGTERM and give in-flight requests up to 10 seconds to finish
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()
	log.Println("shutdown signal received, starting graceful shutdown...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	log.Println("server stopped")
}
