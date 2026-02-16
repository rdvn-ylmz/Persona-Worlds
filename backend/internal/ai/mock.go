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
	return fmt.Sprintf(
		"[%s | draft] In %s, %s perspective: %s. Question to room: what practical steps have worked for you?",
		persona.Name,
		room.Name,
		persona.Tone,
		persona.Bio,
	), nil
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
