package ai

import (
	"context"
	"fmt"
	"strings"
)

type MockClient struct{}

func NewMockClient() *MockClient {
	return &MockClient{}
}

func (m *MockClient) GeneratePostDraft(_ context.Context, persona PersonaContext, room RoomContext) (string, error) {
	language := strings.ToLower(strings.TrimSpace(persona.PreferredLanguage))
	if language != "tr" {
		language = "en"
	}

	insight := "small weekly experiments create compounding product learning"
	if room.Variant == 2 {
		insight = "shipping a visible changelog improves community trust and feedback quality"
	}

	catchphrase := ""
	if len(persona.Catchphrases) > 0 {
		catchphrase = strings.TrimSpace(persona.Catchphrases[0])
	}

	if language == "tr" {
		if catchphrase != "" {
			insight = fmt.Sprintf("%s: %s", catchphrase, insight)
		}
		return fmt.Sprintf("%s odasında %s için içgörü: %s. Sende bu yaklaşımın işe yaradığı bir örnek var mı?", room.Name, persona.Name, insight), nil
	}

	if catchphrase != "" {
		insight = fmt.Sprintf("%s: %s", catchphrase, insight)
	}
	return fmt.Sprintf("In %s, an insight for %s is that %s. What is one concrete example where this worked for you?", room.Name, persona.Name, insight), nil
}

func (m *MockClient) GenerateReply(_ context.Context, persona PersonaContext, post PostContext, thread []ReplyContext) (string, error) {
	threadSize := len(thread)
	return fmt.Sprintf(
		"%s reply (%s): I agree with the direction of the post. My practical addition is to run a small experiment, measure outcomes, and share findings. (thread replies: %d)",
		persona.Name,
		persona.Tone,
		threadSize,
	), nil
}

func (m *MockClient) SummarizeThread(_ context.Context, post PostContext, replies []ReplyContext) (string, error) {
	if len(replies) == 0 {
		return "No replies yet. The thread is waiting for first reactions.", nil
	}

	var snippets []string
	limit := len(replies)
	if limit > 3 {
		limit = 3
	}
	for i := 0; i < limit; i++ {
		snippets = append(snippets, replies[i].Content)
	}

	return fmt.Sprintf("Post focus: %s. Main reply themes: %s", post.Content, strings.Join(snippets, " | ")), nil
}

func (m *MockClient) SummarizePersonaActivity(_ context.Context, persona PersonaContext, stats DigestStats, threads []DigestThreadContext) (string, error) {
	if stats.Posts == 0 && stats.Replies == 0 {
		if strings.ToLower(strings.TrimSpace(persona.PreferredLanguage)) == "tr" {
			return fmt.Sprintf("%s için bugün yeni bir aktivite yok. Uygun olduğunda yeni taslaklar ve yanıtlar burada özetlenecek.", persona.Name), nil
		}
		return fmt.Sprintf("No new activity for %s today yet. New posts and replies will appear here once they happen.", persona.Name), nil
	}

	parts := make([]string, 0, len(threads))
	for i, thread := range threads {
		if i >= 3 {
			break
		}
		label := thread.RoomName
		if strings.TrimSpace(label) == "" {
			label = "room"
		}
		parts = append(parts, fmt.Sprintf("%s (%d events)", label, thread.ActivityCount))
	}

	threadSummary := "no dominant threads yet"
	if len(parts) > 0 {
		threadSummary = strings.Join(parts, ", ")
	}

	if strings.ToLower(strings.TrimSpace(persona.PreferredLanguage)) == "tr" {
		return fmt.Sprintf("Bugün %d gönderi ve %d yanıt üretildi. En dikkat çeken başlıklar: %s.", stats.Posts, stats.Replies, threadSummary), nil
	}

	return fmt.Sprintf("Today the persona produced %d posts and %d replies. The most active threads were: %s.", stats.Posts, stats.Replies, threadSummary), nil
}
