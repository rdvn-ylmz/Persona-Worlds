package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"personaworlds/backend/internal/ai/prompts"
	"strings"
	"time"
)

type OpenAIClient struct {
	apiKey         string
	baseURL        string
	model          string
	requestTimeout time.Duration
	maxRetries     int
	retryBase      time.Duration
	http           *http.Client
}

func NewOpenAIClient(apiKey, baseURL, model string, requestTimeout time.Duration, maxRetries int, retryBase time.Duration) *OpenAIClient {
	if requestTimeout <= 0 {
		requestTimeout = 20 * time.Second
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	if maxRetries > 5 {
		maxRetries = 5
	}
	if retryBase <= 0 {
		retryBase = 400 * time.Millisecond
	}

	return &OpenAIClient{
		apiKey:         apiKey,
		baseURL:        strings.TrimRight(baseURL, "/"),
		model:          model,
		requestTimeout: requestTimeout,
		maxRetries:     maxRetries,
		retryBase:      retryBase,
		http: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

func (c *OpenAIClient) GeneratePostDraft(ctx context.Context, persona PersonaContext, room RoomContext) (string, error) {
	prompt := prompts.PostDraft(
		prompts.Persona{
			Name:              persona.Name,
			Bio:               persona.Bio,
			Tone:              persona.Tone,
			WritingSamples:    persona.WritingSamples,
			DoNotSay:          persona.DoNotSay,
			Catchphrases:      persona.Catchphrases,
			PreferredLanguage: persona.PreferredLanguage,
			Formality:         persona.Formality,
		},
		prompts.Room{
			Name:        room.Name,
			Description: room.Description,
			Variant:     room.Variant,
		},
	)
	return c.chat(ctx, prompt.System, prompt.User)
}

func (c *OpenAIClient) GenerateReply(ctx context.Context, persona PersonaContext, post PostContext, thread []ReplyContext) (string, error) {
	promptThread := make([]prompts.ReplyItem, 0, len(thread))
	for _, reply := range thread {
		promptThread = append(promptThread, prompts.ReplyItem{Content: reply.Content})
	}

	prompt := prompts.Reply(
		prompts.Persona{
			Name: persona.Name,
			Bio:  persona.Bio,
			Tone: persona.Tone,
		},
		prompts.Post{Content: post.Content},
		promptThread,
	)
	return c.chat(ctx, prompt.System, prompt.User)
}

func (c *OpenAIClient) SummarizeThread(ctx context.Context, post PostContext, replies []ReplyContext) (string, error) {
	promptReplies := make([]prompts.ReplyItem, 0, len(replies))
	for _, reply := range replies {
		promptReplies = append(promptReplies, prompts.ReplyItem{Content: reply.Content})
	}

	prompt := prompts.ThreadSummary(prompts.Post{Content: post.Content}, promptReplies)
	return c.chat(ctx, prompt.System, prompt.User)
}

func (c *OpenAIClient) SummarizePersonaActivity(ctx context.Context, persona PersonaContext, stats DigestStats, threads []DigestThreadContext) (string, error) {
	promptThreads := make([]prompts.DigestThread, 0, len(threads))
	for _, thread := range threads {
		promptThreads = append(promptThreads, prompts.DigestThread{
			PostID:        thread.PostID,
			RoomName:      thread.RoomName,
			PostPreview:   thread.PostPreview,
			ActivityCount: thread.ActivityCount,
		})
	}

	prompt := prompts.PersonaActivitySummary(
		prompts.Persona{
			Name:              persona.Name,
			Tone:              persona.Tone,
			PreferredLanguage: persona.PreferredLanguage,
		},
		prompts.DigestStats{
			Posts:   stats.Posts,
			Replies: stats.Replies,
		},
		promptThreads,
	)
	return c.chat(ctx, prompt.System, prompt.User)
}

func (c *OpenAIClient) endpoint() string {
	if strings.HasSuffix(c.baseURL, "/v1") {
		return c.baseURL + "/chat/completions"
	}
	return c.baseURL + "/v1/chat/completions"
}

func (c *OpenAIClient) chat(ctx context.Context, system, user string) (string, error) {
	if c.apiKey == "" {
		return "", errors.New("OPENAI_API_KEY is required for openai provider")
	}
	ctx, cancel := contextWithDefaultTimeout(ctx, c.requestTimeout)
	defer cancel()

	requestBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature": 0.7,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(jsonBody))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		content, retryable, err := c.chatOnce(req)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if !retryable || attempt >= c.maxRetries {
			break
		}

		wait := retryDelay(c.retryBase, attempt)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", lastErr
}

func (c *OpenAIClient) chatOnce(req *http.Request) (string, bool, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", false, err
		}
		return "", true, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(bodySnippet))
		if message == "" {
			message = fmt.Sprintf("status %d", resp.StatusCode)
		}
		err := fmt.Errorf("openai provider error: status=%d body=%s", resp.StatusCode, message)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			return "", true, err
		}
		return "", false, err
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", true, err
	}
	if len(out.Choices) == 0 {
		return "", true, errors.New("openai provider returned no choices")
	}

	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return "", true, errors.New("openai provider returned empty content")
	}
	return content, false, nil
}

func contextWithDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), timeout)
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func retryDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 400 * time.Millisecond
	}
	if attempt < 0 {
		attempt = 0
	}
	delay := base * time.Duration(1<<attempt)
	jitterScale := 0.8 + (rand.Float64() * 0.4)
	jittered := time.Duration(float64(delay) * jitterScale)
	if jittered < 50*time.Millisecond {
		return 50 * time.Millisecond
	}
	return jittered
}
