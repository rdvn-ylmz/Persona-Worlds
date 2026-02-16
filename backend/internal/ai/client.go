package ai

import "context"

type PersonaContext struct {
	ID                string
	Name              string
	Bio               string
	Tone              string
	WritingSamples    []string
	DoNotSay          []string
	Catchphrases      []string
	PreferredLanguage string
	Formality         int
}

type RoomContext struct {
	ID          string
	Name        string
	Description string
	Variant     int
}

type PostContext struct {
	ID      string
	Content string
}

type ReplyContext struct {
	ID      string
	Content string
}

type LLMClient interface {
	GeneratePostDraft(ctx context.Context, persona PersonaContext, room RoomContext) (string, error)
	GenerateReply(ctx context.Context, persona PersonaContext, post PostContext, thread []ReplyContext) (string, error)
	SummarizeThread(ctx context.Context, post PostContext, replies []ReplyContext) (string, error)
}
