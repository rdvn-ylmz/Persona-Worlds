package api

import "time"

type PublicPersonaProfileDTO struct {
	PersonaID         string    `json:"persona_id"`
	Slug              string    `json:"slug"`
	Name              string    `json:"name"`
	Bio               string    `json:"bio"`
	Tone              string    `json:"tone"`
	PreferredLanguage string    `json:"preferred_language"`
	Formality         int       `json:"formality"`
	IsPublic          bool      `json:"is_public"`
	Followers         int       `json:"followers"`
	PostsCount        int       `json:"posts_count"`
	Badges            []string  `json:"badges"`
	CreatedAt         time.Time `json:"created_at"`
}

type PublicPostDTO struct {
	ID         string    `json:"id"`
	RoomID     string    `json:"room_id"`
	RoomName   string    `json:"room_name"`
	AuthoredBy string    `json:"authored_by"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type PublicRoomStatDTO struct {
	RoomID    string `json:"room_id"`
	RoomName  string `json:"room_name"`
	PostCount int    `json:"post_count"`
}

type PublicBattleMetaDTO struct {
	BattleID  string `json:"battle_id"`
	RoomID    string `json:"room_id"`
	RoomName  string `json:"room_name"`
	Topic     string `json:"topic"`
	CreatedAt string `json:"created_at"`
	Template  any    `json:"template,omitempty"`
	ShareURL  string `json:"share_url"`
	CardURL   string `json:"card_url"`
}

func mapPublicProfileDTO(profile PublicPersonaProfile) PublicPersonaProfileDTO {
	badges := make([]string, len(profile.Badges))
	copy(badges, profile.Badges)
	return PublicPersonaProfileDTO{
		PersonaID:         profile.PersonaID,
		Slug:              profile.Slug,
		Name:              profile.Name,
		Bio:               profile.Bio,
		Tone:              profile.Tone,
		PreferredLanguage: profile.PreferredLanguage,
		Formality:         profile.Formality,
		IsPublic:          profile.IsPublic,
		Followers:         profile.Followers,
		PostsCount:        profile.PostsCount,
		Badges:            badges,
		CreatedAt:         profile.CreatedAt,
	}
}

func mapPublicPostsDTO(posts []PublicPost) []PublicPostDTO {
	out := make([]PublicPostDTO, 0, len(posts))
	for _, post := range posts {
		out = append(out, PublicPostDTO{
			ID:         post.ID,
			RoomID:     post.RoomID,
			RoomName:   post.RoomName,
			AuthoredBy: post.AuthoredBy,
			Content:    post.Content,
			CreatedAt:  post.CreatedAt,
		})
	}
	if out == nil {
		return []PublicPostDTO{}
	}
	return out
}

func mapPublicRoomStatsDTO(stats []PublicRoomStat) []PublicRoomStatDTO {
	out := make([]PublicRoomStatDTO, 0, len(stats))
	for _, stat := range stats {
		out = append(out, PublicRoomStatDTO{
			RoomID:    stat.RoomID,
			RoomName:  stat.RoomName,
			PostCount: stat.PostCount,
		})
	}
	if out == nil {
		return []PublicRoomStatDTO{}
	}
	return out
}
