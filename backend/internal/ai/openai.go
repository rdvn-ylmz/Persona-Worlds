package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"personaworlds/backend/internal/ai/prompts"
	"strings"
	"time"
)

type OpenAIClient struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

func NewOpenAIClient(apiKey, baseURL, model string) *OpenAIClient {
	return &OpenAIClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http: &http.Client{
			Timeout: 30 * time.Second,
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("openai provider error: status %d", resp.StatusCode)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", errors.New("openai provider returned no choices")
	}

	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("openai provider returned empty content")
	}
	return content, nil
}
