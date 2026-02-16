package prompts

import (
	"fmt"
	"strings"
)

type Persona struct {
	Name              string
	Bio               string
	Tone              string
	WritingSamples    []string
	DoNotSay          []string
	Catchphrases      []string
	PreferredLanguage string
	Formality         int
}

type Room struct {
	Name        string
	Description string
	Variant     int
}

type Post struct {
	Content string
}

type ReplyItem struct {
	Content string
}

type DigestStats struct {
	Posts   int
	Replies int
}

type DigestThread struct {
	PostID        string
	RoomName      string
	PostPreview   string
	ActivityCount int
}

type ChatPrompt struct {
	System string
	User   string
}

func PostDraft(persona Persona, room Room) ChatPrompt {
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
	return ChatPrompt{System: system, User: user}
}

func Reply(persona Persona, post Post, thread []ReplyItem) ChatPrompt {
	var threadLines []string
	for _, reply := range thread {
		threadLines = append(threadLines, reply.Content)
	}
	system := "You create one short, constructive social reply for a persona."
	user := fmt.Sprintf("Persona: %s\nBio: %s\nTone: %s\nPost: %s\nThread: %s\nGenerate one reply in <=90 words.", persona.Name, persona.Bio, persona.Tone, post.Content, strings.Join(threadLines, "\n- "))
	return ChatPrompt{System: system, User: user}
}

func ThreadSummary(post Post, replies []ReplyItem) ChatPrompt {
	var parts []string
	for _, reply := range replies {
		parts = append(parts, reply.Content)
	}
	system := "You summarize threads in a few bullet-like sentences with neutral tone."
	user := fmt.Sprintf("Post: %s\nReplies: %s\nProvide a compact summary in <=120 words.", post.Content, strings.Join(parts, "\n- "))
	return ChatPrompt{System: system, User: user}
}

func PersonaActivitySummary(persona Persona, stats DigestStats, threads []DigestThread) ChatPrompt {
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
	return ChatPrompt{System: system, User: user}
}

func formatStringList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, " | ")
}
