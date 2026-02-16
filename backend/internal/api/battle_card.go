package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	imagedraw "image/draw"
	"image/png"
	"net/http"
	"strings"
	"sync"
	"time"

	"personaworlds/backend/internal/common"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	battleCardWidth  = 960
	battleCardHeight = 540
)

type battleCardData struct {
	BattleID   string
	RoomName   string
	Topic      string
	ProPersona string
	ConPersona string
	Verdict    string
	Takeaways  []string
	URL        string
	UpdatedAt  time.Time
}

type battleCardReply struct {
	PersonaName string
	Content     string
	UpdatedAt   time.Time
}

type battleCardCache struct {
	mu         sync.Mutex
	items      map[string][]byte
	order      []string
	maxEntries int
}

func newBattleCardCache(maxEntries int) *battleCardCache {
	if maxEntries <= 0 {
		maxEntries = 128
	}
	return &battleCardCache{
		items:      map[string][]byte{},
		order:      make([]string, 0, maxEntries),
		maxEntries: maxEntries,
	}
}

func (c *battleCardCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value, ok := c.items[key]
	if !ok {
		return nil, false
	}
	return value, true
}

func (c *battleCardCache) set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.items[key]; exists {
		c.items[key] = value
		return
	}

	c.items[key] = value
	c.order = append(c.order, key)

	for len(c.order) > c.maxEntries {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.items, oldest)
	}
}

func (s *Server) handleGetBattleCardImage(w http.ResponseWriter, r *http.Request) {
	battleID, err := validateUUID(chi.URLParam(r, "id"), "battle id")
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}

	card, err := s.loadBattleCardData(r.Context(), battleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeNotFound(w, "battle not found")
			return
		}
		writeInternalError(w, "could not build battle card")
		return
	}

	cacheKey := fmt.Sprintf("%s|%d", card.BattleID, card.UpdatedAt.UTC().UnixNano())
	if cached, ok := s.battleCardCache.get(cacheKey); ok {
		writeBattleCardPNG(w, cached, cacheKey)
		return
	}

	imageBytes, err := renderBattleCardPNG(card)
	if err != nil {
		writeInternalError(w, "could not render battle card")
		return
	}

	s.battleCardCache.set(cacheKey, imageBytes)
	writeBattleCardPNG(w, imageBytes, cacheKey)
}

func writeBattleCardPNG(w http.ResponseWriter, payload []byte, cacheKey string) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("ETag", fmt.Sprintf(`"battle-card-%s"`, cacheKey))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) loadBattleCardData(ctx context.Context, battleID string) (battleCardData, error) {
	var (
		data            battleCardData
		postContent     string
		postPersonaName string
	)

	err := s.db.QueryRow(ctx, `
		SELECT
			p.id::text,
			COALESCE(rm.name, ''),
			p.content,
			p.updated_at,
			COALESCE(pr.name, '')
		FROM posts p
		JOIN rooms rm ON rm.id = p.room_id
		LEFT JOIN personas pr ON pr.id = p.persona_id
		WHERE p.id = $1
		  AND p.status = 'PUBLISHED'
	`, battleID).Scan(
		&data.BattleID,
		&data.RoomName,
		&postContent,
		&data.UpdatedAt,
		&postPersonaName,
	)
	if err != nil {
		return battleCardData{}, err
	}

	replies, err := s.listBattleCardReplies(ctx, battleID)
	if err != nil {
		return battleCardData{}, err
	}
	for _, reply := range replies {
		if reply.UpdatedAt.After(data.UpdatedAt) {
			data.UpdatedAt = reply.UpdatedAt
		}
	}

	data.Topic = buildBattleCardTopic(postContent, data.RoomName)
	data.ProPersona, data.ConPersona = resolveBattleCardPersonaSides(postPersonaName, replies)
	data.Verdict = buildBattleCardVerdict(replies)
	data.Takeaways = buildBattleCardTakeaways(postContent, replies)
	data.URL = fmt.Sprintf("%s/b/%s", strings.TrimRight(s.cfg.FrontendOrigin, "/"), data.BattleID)
	return data, nil
}

func (s *Server) listBattleCardReplies(ctx context.Context, battleID string) ([]battleCardReply, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			COALESCE(p.name, ''),
			r.content,
			r.updated_at
		FROM replies r
		LEFT JOIN personas p ON p.id = r.persona_id
		WHERE r.post_id = $1
		ORDER BY r.created_at ASC
	`, battleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	replies := make([]battleCardReply, 0, 6)
	for rows.Next() {
		var reply battleCardReply
		if err := rows.Scan(&reply.PersonaName, &reply.Content, &reply.UpdatedAt); err != nil {
			return nil, err
		}
		replies = append(replies, reply)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return replies, nil
}

func buildBattleCardTopic(postContent, roomName string) string {
	topic := extractCardSentence(postContent, 90)
	if topic == "" {
		topic = "Battle discussion"
	}
	if strings.TrimSpace(roomName) != "" {
		return common.TruncateRunes(fmt.Sprintf("%s (%s)", topic, strings.TrimSpace(roomName)), 96)
	}
	return topic
}

func resolveBattleCardPersonaSides(postPersonaName string, replies []battleCardReply) (string, string) {
	names := make([]string, 0, len(replies)+1)
	seen := map[string]struct{}{}

	add := func(name string) {
		clean := common.TruncateRunes(strings.TrimSpace(name), 36)
		if clean == "" {
			return
		}
		key := strings.ToLower(clean)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		names = append(names, clean)
	}

	add(postPersonaName)
	for _, reply := range replies {
		add(reply.PersonaName)
	}

	pro := "Persona A"
	con := "Persona B"
	if len(names) > 0 {
		pro = names[0]
	}
	if len(names) > 1 {
		con = names[1]
	} else if len(names) == 1 {
		con = "Community"
	}
	return pro, con
}

func buildBattleCardVerdict(replies []battleCardReply) string {
	switch {
	case len(replies) >= 2:
		return "Verdict: The strongest outcome is to run a small test, measure results, and iterate fast."
	case len(replies) == 1:
		return "Verdict: The counterpoint matters, but practical experimentation still wins."
	default:
		return "Verdict: Start lean, validate quickly, and scale only what proves useful."
	}
}

func buildBattleCardTakeaways(postContent string, replies []battleCardReply) []string {
	takeaways := make([]string, 0, 3)
	seen := map[string]struct{}{}
	add := func(value string) {
		clean := common.TruncateRunes(extractCardSentence(value, 90), 90)
		if clean == "" {
			return
		}
		key := strings.ToLower(clean)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		takeaways = append(takeaways, clean)
	}

	add(postContent)
	for _, reply := range replies {
		add(reply.Content)
		if len(takeaways) == 3 {
			break
		}
	}

	fallbacks := []string{
		"Keep claims concrete and tied to measurable outcomes.",
		"Compare both sides with one clear experiment.",
		"Share the result publicly and refine the next iteration.",
	}
	for _, fallback := range fallbacks {
		if len(takeaways) == 3 {
			break
		}
		add(fallback)
	}

	for len(takeaways) < 3 {
		takeaways = append(takeaways, fallbacks[len(takeaways)%len(fallbacks)])
	}
	return takeaways[:3]
}

func extractCardSentence(value string, maxRunes int) string {
	clean := normalizeCardText(value)
	if clean == "" {
		return ""
	}

	end := len(clean)
	for idx, r := range clean {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			end = idx + 1
			break
		}
	}
	sentence := strings.TrimSpace(clean[:end])
	sentence = strings.Trim(sentence, "\"' ")
	return common.TruncateRunes(sentence, maxRunes)
}

func normalizeCardText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.Join(strings.Fields(value), " ")
}

func renderBattleCardPNG(data battleCardData) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, battleCardWidth, battleCardHeight))

	background := color.RGBA{R: 15, G: 23, B: 42, A: 255}
	panel := color.RGBA{R: 248, G: 250, B: 252, A: 255}
	panelMuted := color.RGBA{R: 241, G: 245, B: 249, A: 255}
	ink := color.RGBA{R: 15, G: 23, B: 42, A: 255}
	inkMuted := color.RGBA{R: 71, G: 85, B: 105, A: 255}
	accent := color.RGBA{R: 59, G: 130, B: 246, A: 255}
	accentSoft := color.RGBA{R: 219, G: 234, B: 254, A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	fillRect(canvas, canvas.Bounds(), background)

	titleRect := image.Rect(24, 24, battleCardWidth-24, 150)
	leftRect := image.Rect(24, 166, 644, 440)
	rightRect := image.Rect(660, 166, battleCardWidth-24, 440)
	footerRect := image.Rect(24, 456, battleCardWidth-24, battleCardHeight-24)

	fillRect(canvas, titleRect, panel)
	fillRect(canvas, leftRect, panel)
	fillRect(canvas, rightRect, panelMuted)
	fillRect(canvas, footerRect, accentSoft)
	strokeRect(canvas, titleRect, accent)
	strokeRect(canvas, leftRect, color.RGBA{R: 148, G: 163, B: 184, A: 255})
	strokeRect(canvas, rightRect, color.RGBA{R: 148, G: 163, B: 184, A: 255})
	strokeRect(canvas, footerRect, accent)

	face := basicfont.Face7x13
	lineHeight := 18

	drawLabel(canvas, face, 40, 46, "BATTLE CARD", accent)
	drawWrappedText(
		canvas,
		face,
		40,
		74,
		titleRect.Dx()-32,
		lineHeight,
		4,
		data.Topic,
		ink,
	)

	drawLabel(canvas, face, 40, 188, "PRO / CON", accent)
	personaLine := fmt.Sprintf("Pro: %s    Con: %s", data.ProPersona, data.ConPersona)
	drawWrappedText(canvas, face, 40, 214, leftRect.Dx()-32, lineHeight, 2, personaLine, ink)

	drawLabel(canvas, face, 40, 254, "VERDICT", accent)
	drawWrappedText(canvas, face, 40, 280, leftRect.Dx()-32, lineHeight, 5, data.Verdict, ink)

	drawLabel(canvas, face, 676, 188, "TOP TAKEAWAYS", accent)
	takeawayY := 214
	for idx, takeaway := range data.Takeaways {
		line := fmt.Sprintf("%d. %s", idx+1, takeaway)
		takeawayY = drawWrappedText(
			canvas,
			face,
			676,
			takeawayY,
			rightRect.Dx()-32,
			lineHeight,
			3,
			line,
			ink,
		) + 4
	}

	drawLabel(canvas, face, 40, 478, "SHARE LINK", accent)
	linkDisplay := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(data.URL), "https://"), "http://")
	drawWrappedText(canvas, face, 40, 504, footerRect.Dx()-32, lineHeight, 2, linkDisplay, inkMuted)
	drawLabel(canvas, face, battleCardWidth-210, 478, "OPEN BATTLE", accent)
	drawWrappedText(canvas, face, battleCardWidth-210, 504, 170, lineHeight, 1, data.BattleID, white)

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fillRect(img *image.RGBA, rect image.Rectangle, c color.Color) {
	imagedraw.Draw(img, rect, &image.Uniform{C: c}, image.Point{}, imagedraw.Src)
}

func strokeRect(img *image.RGBA, rect image.Rectangle, c color.Color) {
	top := image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+1)
	bottom := image.Rect(rect.Min.X, rect.Max.Y-1, rect.Max.X, rect.Max.Y)
	left := image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+1, rect.Max.Y)
	right := image.Rect(rect.Max.X-1, rect.Min.Y, rect.Max.X, rect.Max.Y)
	fillRect(img, top, c)
	fillRect(img, bottom, c)
	fillRect(img, left, c)
	fillRect(img, right, c)
}

func drawLabel(img *image.RGBA, face font.Face, x, y int, label string, c color.Color) {
	drawText(img, face, x, y, strings.ToUpper(strings.TrimSpace(label)), c)
}

func drawText(img *image.RGBA, face font.Face, x, y int, text string, c color.Color) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	drawer := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	drawer.DrawString(text)
}

func drawWrappedText(
	img *image.RGBA,
	face font.Face,
	x int,
	y int,
	maxWidth int,
	lineHeight int,
	maxLines int,
	value string,
	c color.Color,
) int {
	if lineHeight <= 0 {
		lineHeight = 16
	}
	if maxLines <= 0 {
		maxLines = 1
	}

	lines := wrapCardText(value, face, maxWidth)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		last := strings.TrimSpace(lines[len(lines)-1])
		if !strings.HasSuffix(last, "...") {
			last = common.TruncateRunes(last, 60) + "..."
		}
		lines[len(lines)-1] = last
	}

	currentY := y
	for _, line := range lines {
		drawText(img, face, x, currentY, line, c)
		currentY += lineHeight
	}
	return currentY
}

func wrapCardText(value string, face font.Face, maxWidth int) []string {
	clean := normalizeCardText(value)
	if clean == "" {
		return []string{""}
	}
	if maxWidth <= 8 {
		return []string{clean}
	}

	words := strings.Fields(clean)
	if len(words) == 0 {
		return []string{""}
	}

	lines := make([]string, 0, 4)
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if measureCardTextWidth(face, candidate) <= maxWidth {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word

		if measureCardTextWidth(face, current) > maxWidth {
			parts := splitCardLongWord(current, face, maxWidth)
			lines = append(lines, parts[:len(parts)-1]...)
			current = parts[len(parts)-1]
		}
	}
	if current != "" {
		lines = append(lines, current)
	}

	return lines
}

func splitCardLongWord(word string, face font.Face, maxWidth int) []string {
	chars := []rune(strings.TrimSpace(word))
	if len(chars) == 0 {
		return []string{""}
	}

	parts := make([]string, 0, 2)
	start := 0
	for start < len(chars) {
		end := start + 1
		for end <= len(chars) {
			chunk := string(chars[start:end])
			if measureCardTextWidth(face, chunk) > maxWidth {
				break
			}
			end++
		}

		if end == start+1 {
			end = start + 1
		} else {
			end--
		}
		parts = append(parts, string(chars[start:end]))
		start = end
	}
	return parts
}

func measureCardTextWidth(face font.Face, value string) int {
	return font.MeasureString(face, value).Ceil()
}
