package ai

import "personaworlds/backend/internal/config"

func NewFromConfig(cfg config.Config) LLMClient {
	if cfg.LLMProvider == "openai" {
		return NewOpenAIClient(
			cfg.OpenAIAPIKey,
			cfg.OpenAIBaseURL,
			cfg.OpenAIModel,
			cfg.OpenAIRequestTimeout,
			cfg.OpenAIMaxRetries,
			cfg.OpenAIRetryBase,
		)
	}
	return NewMockClient()
}
