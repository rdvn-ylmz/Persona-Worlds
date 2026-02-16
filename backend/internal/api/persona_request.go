package api

import (
	"encoding/json"
	"fmt"
)

type personaUpsertRequest struct {
	Name              string   `json:"name"`
	Bio               string   `json:"bio"`
	Tone              string   `json:"tone"`
	WritingSamples    []string `json:"writing_samples"`
	DoNotSay          []string `json:"do_not_say"`
	Catchphrases      []string `json:"catchphrases"`
	PreferredLanguage string   `json:"preferred_language"`
	Formality         int      `json:"formality"`
	DailyDraftQuota   int      `json:"daily_draft_quota"`
	DailyReplyQuota   int      `json:"daily_reply_quota"`
}

func (r *personaUpsertRequest) applyDefaultQuotas(defaultDraft, defaultReply int) {
	if r.DailyDraftQuota <= 0 {
		r.DailyDraftQuota = defaultDraft
	}
	if r.DailyReplyQuota <= 0 {
		r.DailyReplyQuota = defaultReply
	}
}

func (r personaUpsertRequest) validatePositiveQuotas() error {
	if r.DailyDraftQuota <= 0 || r.DailyReplyQuota <= 0 {
		return fmt.Errorf("quotas must be positive")
	}
	return nil
}

func (r personaUpsertRequest) normalizedPersonaInput() (personaInput, error) {
	return normalizePersonaInput(
		r.Name,
		r.Bio,
		r.Tone,
		r.WritingSamples,
		r.DoNotSay,
		r.Catchphrases,
		r.PreferredLanguage,
		r.Formality,
	)
}

func marshalPersonaJSONFields(input personaInput) (writingSamplesJSON, doNotSayJSON, catchphrasesJSON []byte) {
	writingSamplesJSON, _ = json.Marshal(input.WritingSamples)
	doNotSayJSON, _ = json.Marshal(input.DoNotSay)
	catchphrasesJSON, _ = json.Marshal(input.Catchphrases)
	return writingSamplesJSON, doNotSayJSON, catchphrasesJSON
}
