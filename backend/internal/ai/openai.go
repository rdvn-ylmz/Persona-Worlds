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

func (c *OpenAIClient) GenerateBattleTurn(ctx context.Context, input BattleTurnInput) (BattleTurnOutput, error) {
	historyLines := make([]string, 0, len(input.History))
	for _, turn := range input.History {
		historyLines = append(historyLines, fmt.Sprintf(
			"Turn %d | %s | %s | Claim: %s | Evidence: %s",
			turn.TurnIndex,
			turn.PersonaName,
			turn.Side,
			turn.Claim,
			turn.Evidence,
		))
	}
	if len(historyLines) == 0 {
		historyLines = append(historyLines, "No previous turns")
	}

	system := "You write one concise debate turn in strict JSON."
	user := fmt.Sprintf(
		"Topic: %s\nTurn index: %d\nPersona name: %s\nPersona bio: %s\nPersona tone: %s\nOpponent: %s\nSide: %s\nPrior turns: %s\nOutput JSON rules: return exactly one object with keys claim and evidence. claim must be one sentence. evidence must be one sentence with concrete support. No markdown or extra keys.",
		input.Topic,
		input.TurnIndex,
		input.Persona.Name,
		input.Persona.Bio,
		input.Persona.Tone,
		input.Opponent.Name,
		strings.ToUpper(strings.TrimSpace(input.Side)),
		strings.Join(historyLines, "\n- "),
	)

	raw, err := c.chat(ctx, system, user)
	if err != nil {
		return BattleTurnOutput{}, err
	}

	var out BattleTurnOutput
	if err := parseJSONObject(raw, &out); err != nil {
		return BattleTurnOutput{}, err
	}
	out.Claim = strings.TrimSpace(out.Claim)
	out.Evidence = strings.TrimSpace(out.Evidence)
	if out.Claim == "" || out.Evidence == "" {
		return BattleTurnOutput{}, errors.New("openai provider returned invalid battle turn")
	}
	return out, nil
}

func (c *OpenAIClient) GenerateBattleVerdict(ctx context.Context, input BattleVerdictInput) (BattleVerdict, error) {
	turnLines := make([]string, 0, len(input.Turns))
	for _, turn := range input.Turns {
		turnLines = append(turnLines, fmt.Sprintf(
			"Turn %d | %s | %s | Claim: %s | Evidence: %s",
			turn.TurnIndex,
			turn.PersonaName,
			turn.Side,
			turn.Claim,
			turn.Evidence,
		))
	}
	if len(turnLines) == 0 {
		turnLines = append(turnLines, "No turns")
	}

	system := "You judge a short debate and respond in strict JSON."
	user := fmt.Sprintf(
		"Topic: %s\nPersona A: %s\nPersona B: %s\nTurns: %s\nOutput JSON rules: return exactly one object with keys verdict (string) and takeaways (array of exactly 3 short strings). No markdown, no extra keys.",
		input.Topic,
		input.PersonaA.Name,
		input.PersonaB.Name,
		strings.Join(turnLines, "\n- "),
	)

	raw, err := c.chat(ctx, system, user)
	if err != nil {
		return BattleVerdict{}, err
	}

	var out BattleVerdict
	if err := parseJSONObject(raw, &out); err != nil {
		return BattleVerdict{}, err
	}
	out.Verdict = strings.TrimSpace(out.Verdict)
	cleanTakeaways := make([]string, 0, 3)
	for _, takeaway := range out.Takeaways {
		trimmed := strings.TrimSpace(takeaway)
		if trimmed == "" {
			continue
		}
		cleanTakeaways = append(cleanTakeaways, trimmed)
		if len(cleanTakeaways) == 3 {
			break
		}
	}
	out.Takeaways = cleanTakeaways
	if out.Verdict == "" || len(out.Takeaways) == 0 {
		return BattleVerdict{}, errors.New("openai provider returned invalid battle verdict")
	}
	return out, nil
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

func parseJSONObject(raw string, dst any) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("empty model response")
	}

	if err := json.Unmarshal([]byte(trimmed), dst); err == nil {
		return nil
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end <= start {
		return errors.New("model response did not contain json object")
	}

	candidate := trimmed[start : end+1]
	if err := json.Unmarshal([]byte(candidate), dst); err != nil {
		return err
	}
	return nil
}
