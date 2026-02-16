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
