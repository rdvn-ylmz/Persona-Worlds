package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	system := "You create concise social posts for an AI persona. Keep output non-spam, no links, and no hashtag stuffing."
	user := fmt.Sprintf(
		"Persona: %s\nBio: %s\nTone: %s\nPreferred language: %s\nFormality (0 casual - 3 formal): %d\nWriting samples: %s\nDo not say list: %s\nCatchphrases: %s\nRoom: %s\nRoom Description: %s\nVariant: %d\nOutput rules: <= 90 words, exactly two sentences, first sentence has one practical insight, second sentence has one question. Avoid banned phrases and do not sound promotional.",
		persona.Name,
		persona.Bio,
		persona.Tone,
		persona.PreferredLanguage,
		persona.Formality,
		formatStringList(persona.WritingSamples),
		formatStringList(persona.DoNotSay),
		formatStringList(persona.Catchphrases),
		room.Name,
		room.Description,
		room.Variant,
	)
	return c.chat(ctx, system, user)
}

func (c *OpenAIClient) GenerateReply(ctx context.Context, persona PersonaContext, post PostContext, thread []ReplyContext) (string, error) {
	var threadLines []string
	for _, r := range thread {
		threadLines = append(threadLines, r.Content)
	}
	system := "You create one short, constructive social reply for a persona."
	user := fmt.Sprintf("Persona: %s\nBio: %s\nTone: %s\nPost: %s\nThread: %s\nGenerate one reply in <=90 words.", persona.Name, persona.Bio, persona.Tone, post.Content, strings.Join(threadLines, "\n- "))
	return c.chat(ctx, system, user)
}

func (c *OpenAIClient) SummarizeThread(ctx context.Context, post PostContext, replies []ReplyContext) (string, error) {
	var parts []string
	for _, reply := range replies {
		parts = append(parts, reply.Content)
	}
	system := "You summarize threads in a few bullet-like sentences with neutral tone."
	user := fmt.Sprintf("Post: %s\nReplies: %s\nProvide a compact summary in <=120 words.", post.Content, strings.Join(parts, "\n- "))
	return c.chat(ctx, system, user)
}

func (c *OpenAIClient) SummarizePersonaActivity(ctx context.Context, persona PersonaContext, stats DigestStats, threads []DigestThreadContext) (string, error) {
	threadLines := make([]string, 0, len(threads))
	for _, thread := range threads {
		threadLines = append(threadLines, fmt.Sprintf("post_id=%s | room=%s | activity=%d | preview=%s", thread.PostID, thread.RoomName, thread.ActivityCount, thread.PostPreview))
	}
	if len(threadLines) == 0 {
		threadLines = append(threadLines, "No active threads")
	}

	system := "You write one concise digest paragraph describing what happened while the user was away."
	user := fmt.Sprintf(
		"Persona: %s\nTone: %s\nPreferred language: %s\nStats today: posts=%d, replies=%d\nTop threads: %s\nOutput rules: 1 paragraph, <=120 words, concrete and neutral, mention thread themes.",
		persona.Name,
		persona.Tone,
		persona.PreferredLanguage,
		stats.Posts,
		stats.Replies,
		strings.Join(threadLines, "\n- "),
	)
	return c.chat(ctx, system, user)
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

func formatStringList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, " | ")
}
