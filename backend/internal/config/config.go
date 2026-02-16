package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port                    string
	DatabaseURL             string
	JWTSecret               string
	LLMProvider             string
	OpenAIBaseURL           string
	OpenAIAPIKey            string
	OpenAIModel             string
	MigrationsDir           string
	DraftMaxLen             int
	ReplyMaxLen             int
	SummaryMaxLen           int
	DefaultDraftQuota       int
	DefaultReplyQuota       int
	DefaultPreviewQuota     int
	FrontendOrigin          string
	WorkerPollEvery         time.Duration
	WorkerObservabilityPort string
}

func Load() Config {
	return Config{
		Port:                    getEnv("PORT", "8080"),
		DatabaseURL:             getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/personaworlds?sslmode=disable"),
		JWTSecret:               getEnv("JWT_SECRET", "change-me"),
		LLMProvider:             getEnv("LLM_PROVIDER", "mock"),
		OpenAIBaseURL:           getEnv("OPENAI_BASE_URL", "https://api.openai.com"),
		OpenAIAPIKey:            os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:             getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		MigrationsDir:           getEnv("MIGRATIONS_DIR", "./migrations"),
		DraftMaxLen:             getEnvInt("DRAFT_MAX_LEN", 500),
		ReplyMaxLen:             getEnvInt("REPLY_MAX_LEN", 280),
		SummaryMaxLen:           getEnvInt("SUMMARY_MAX_LEN", 400),
		DefaultDraftQuota:       getEnvInt("DEFAULT_DRAFT_QUOTA", 5),
		DefaultReplyQuota:       getEnvInt("DEFAULT_REPLY_QUOTA", 25),
		DefaultPreviewQuota:     getEnvInt("DEFAULT_PREVIEW_QUOTA", 5),
		FrontendOrigin:          getEnv("FRONTEND_ORIGIN", "http://localhost:3000"),
		WorkerPollEvery:         getEnvDuration("WORKER_POLL_EVERY", 3*time.Second),
		WorkerObservabilityPort: getEnv("WORKER_OBSERVABILITY_PORT", "9091"),
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
