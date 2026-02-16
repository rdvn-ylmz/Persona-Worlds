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

type BattleTurnContext struct {
	TurnIndex   int
	PersonaName string
	Side        string
	Claim       string
	Evidence    string
}

type BattleTurnInput struct {
	Topic     string
	Persona   PersonaContext
	Opponent  PersonaContext
	Side      string
	TurnIndex int
	History   []BattleTurnContext
}

type BattleTurnOutput struct {
	Claim    string `json:"claim"`
	Evidence string `json:"evidence"`
}

type BattleVerdictInput struct {
	Topic    string
	PersonaA PersonaContext
	PersonaB PersonaContext
	Turns    []BattleTurnContext
}

type BattleVerdict struct {
	Verdict   string   `json:"verdict"`
	Takeaways []string `json:"takeaways"`
}

type DigestStats struct {
	Posts   int
	Replies int
}

type DigestThreadContext struct {
	PostID        string
	RoomName      string
	PostPreview   string
	ActivityCount int
}

type LLMClient interface {
	GeneratePostDraft(ctx context.Context, persona PersonaContext, room RoomContext) (string, error)
	GenerateReply(ctx context.Context, persona PersonaContext, post PostContext, thread []ReplyContext) (string, error)
	SummarizeThread(ctx context.Context, post PostContext, replies []ReplyContext) (string, error)
	SummarizePersonaActivity(ctx context.Context, persona PersonaContext, stats DigestStats, threads []DigestThreadContext) (string, error)
	GenerateBattleTurn(ctx context.Context, input BattleTurnInput) (BattleTurnOutput, error)
	GenerateBattleVerdict(ctx context.Context, input BattleVerdictInput) (BattleVerdict, error)
}
