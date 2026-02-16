package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv                  string
	Port                    string
	DatabaseURL             string
	JWTSecret               string
	LLMProvider             string
	OpenAIBaseURL           string
	OpenAIAPIKey            string
	OpenAIModel             string
	OpenAIRequestTimeout    time.Duration
	OpenAIMaxRetries        int
	OpenAIRetryBase         time.Duration
	MigrationsDir           string
	DraftMaxLen             int
	ReplyMaxLen             int
	SummaryMaxLen           int
	DefaultDraftQuota       int
	DefaultReplyQuota       int
	DefaultPreviewQuota     int
	FrontendOrigin          string
	CORSAllowedOrigins      []string
	RequestBodyMaxBytes     int64
	PublicBodyMaxBytes      int64
	APIRequestTimeout       time.Duration
	APIReadTimeout          time.Duration
	APIWriteTimeout         time.Duration
	APIIdleTimeout          time.Duration
	DBQueryTimeout          time.Duration
	WorkerPollEvery         time.Duration
	WorkerTaskTimeout       time.Duration
	WorkerObservabilityPort string
	SecureCookies           bool
	JobMaxAttempts          int
	JobRetryBase            time.Duration
	JobRetryMax             time.Duration
}

func Load() Config {
	appEnv := strings.ToLower(strings.TrimSpace(getEnv("APP_ENV", "dev")))
	secureCookies := getEnvBool("SECURE_COOKIES", appEnv == "prod" || appEnv == "production")

	frontendOrigin := getEnv("FRONTEND_ORIGIN", "http://localhost:3000")
	corsAllowedOrigins := parseCSVEnv("CORS_ALLOWED_ORIGINS")
	if len(corsAllowedOrigins) == 0 {
		corsAllowedOrigins = []string{frontendOrigin}
		if appEnv != "prod" && appEnv != "production" {
			corsAllowedOrigins = append(corsAllowedOrigins, "http://localhost:3000", "http://127.0.0.1:3000")
		}
	}

	return Config{
		AppEnv:                  appEnv,
		Port:                    getEnv("PORT", "8080"),
		DatabaseURL:             getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/personaworlds?sslmode=disable"),
		JWTSecret:               getEnv("JWT_SECRET", "change-me"),
		LLMProvider:             getEnv("LLM_PROVIDER", "mock"),
		OpenAIBaseURL:           getEnv("OPENAI_BASE_URL", "https://api.openai.com"),
		OpenAIAPIKey:            os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:             getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIRequestTimeout:    getEnvDuration("OPENAI_REQUEST_TIMEOUT", 20*time.Second),
		OpenAIMaxRetries:        getEnvInt("OPENAI_MAX_RETRIES", 2),
		OpenAIRetryBase:         getEnvDuration("OPENAI_RETRY_BASE", 400*time.Millisecond),
		MigrationsDir:           getEnv("MIGRATIONS_DIR", "./migrations"),
		DraftMaxLen:             getEnvInt("DRAFT_MAX_LEN", 500),
		ReplyMaxLen:             getEnvInt("REPLY_MAX_LEN", 280),
		SummaryMaxLen:           getEnvInt("SUMMARY_MAX_LEN", 400),
		DefaultDraftQuota:       getEnvInt("DEFAULT_DRAFT_QUOTA", 5),
		DefaultReplyQuota:       getEnvInt("DEFAULT_REPLY_QUOTA", 25),
		DefaultPreviewQuota:     getEnvInt("DEFAULT_PREVIEW_QUOTA", 5),
		FrontendOrigin:          frontendOrigin,
		CORSAllowedOrigins:      corsAllowedOrigins,
		RequestBodyMaxBytes:     int64(getEnvInt("REQUEST_BODY_MAX_BYTES", 1<<20)),
		PublicBodyMaxBytes:      int64(getEnvInt("PUBLIC_BODY_MAX_BYTES", 64<<10)),
		APIRequestTimeout:       getEnvDuration("API_REQUEST_TIMEOUT", 15*time.Second),
		APIReadTimeout:          getEnvDuration("API_READ_TIMEOUT", 15*time.Second),
		APIWriteTimeout:         getEnvDuration("API_WRITE_TIMEOUT", 30*time.Second),
		APIIdleTimeout:          getEnvDuration("API_IDLE_TIMEOUT", 60*time.Second),
		DBQueryTimeout:          getEnvDuration("DB_QUERY_TIMEOUT", 5*time.Second),
		WorkerPollEvery:         getEnvDuration("WORKER_POLL_EVERY", 3*time.Second),
		WorkerTaskTimeout:       getEnvDuration("WORKER_TASK_TIMEOUT", 15*time.Second),
		WorkerObservabilityPort: getEnv("WORKER_OBSERVABILITY_PORT", "9091"),
		SecureCookies:           secureCookies,
		JobMaxAttempts:          getEnvInt("JOB_MAX_ATTEMPTS", 5),
		JobRetryBase:            getEnvDuration("JOB_RETRY_BASE", 30*time.Second),
		JobRetryMax:             getEnvDuration("JOB_RETRY_MAX", 10*time.Minute),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseCSVEnv(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		clean := strings.TrimSpace(part)
		if clean == "" {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		items = append(items, clean)
	}
	return items
}
