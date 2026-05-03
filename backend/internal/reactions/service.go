package reactions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Service struct {
	db *sql.DB
}

type Reaction struct {
	ID                          int64     `json:"id"`
	Reaction                    string    `json:"reaction"`
	Title                       string    `json:"title"`
	Enabled                     bool      `json:"enabled"`
	PremiumOnly                 bool      `json:"premiumOnly"`
	SortOrder                   int32     `json:"sortOrder"`
	StaticIconDocumentID        int64     `json:"staticIconDocumentId"`
	AppearAnimationDocumentID   int64     `json:"appearAnimationDocumentId"`
	SelectAnimationDocumentID   int64     `json:"selectAnimationDocumentId"`
	ActivateAnimationDocumentID int64     `json:"activateAnimationDocumentId"`
	EffectAnimationDocumentID   int64     `json:"effectAnimationDocumentId"`
	AroundAnimationDocumentID   int64     `json:"aroundAnimationDocumentId"`
	CenterIconDocumentID        int64     `json:"centerIconDocumentId"`
	CreatedAt                   time.Time `json:"createdAt"`
	UpdatedAt                   time.Time `json:"updatedAt"`
}

type ReactionPatch struct {
	Title                       *string `json:"title,omitempty"`
	Enabled                     *bool   `json:"enabled,omitempty"`
	PremiumOnly                 *bool   `json:"premiumOnly,omitempty"`
	SortOrder                   *int32  `json:"sortOrder,omitempty"`
	StaticIconDocumentID        *int64  `json:"staticIconDocumentId,omitempty"`
	AppearAnimationDocumentID   *int64  `json:"appearAnimationDocumentId,omitempty"`
	SelectAnimationDocumentID   *int64  `json:"selectAnimationDocumentId,omitempty"`
	ActivateAnimationDocumentID *int64  `json:"activateAnimationDocumentId,omitempty"`
	EffectAnimationDocumentID   *int64  `json:"effectAnimationDocumentId,omitempty"`
	AroundAnimationDocumentID   *int64  `json:"aroundAnimationDocumentId,omitempty"`
	CenterIconDocumentID        *int64  `json:"centerIconDocumentId,omitempty"`
}

type EmojiKeyword struct {
	LangCode  string    `json:"langCode"`
	Keyword   string    `json:"keyword"`
	Emoticons []string  `json:"emoticons"`
	Version   int32     `json:"version"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var defaultReactions = []Reaction{
	{Reaction: "❤️", Title: "Love", Enabled: true, SortOrder: 1},
	{Reaction: "😂", Title: "Laugh", Enabled: true, SortOrder: 2},
	{Reaction: "👍", Title: "Like", Enabled: true, SortOrder: 3},
	{Reaction: "👎", Title: "Dislike", Enabled: true, SortOrder: 4},
	{Reaction: "🔥", Title: "Fire", Enabled: true, SortOrder: 5},
	{Reaction: "🎉", Title: "Celebrate", Enabled: true, SortOrder: 6},
	{Reaction: "😢", Title: "Sad", Enabled: true, SortOrder: 7},
	{Reaction: "😮", Title: "Wow", Enabled: true, SortOrder: 8},
}

var defaultKeywords = []EmojiKeyword{
	{LangCode: "zh", Keyword: "笑", Emoticons: []string{"😂", "🤣", "😆"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "大笑", Emoticons: []string{"😂", "🤣"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "哈哈", Emoticons: []string{"😂", "🤣"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "爱", Emoticons: []string{"❤️", "😍", "🥰"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "喜欢", Emoticons: []string{"❤️", "😍", "👍"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "心", Emoticons: []string{"❤️", "💖"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "火", Emoticons: []string{"🔥", "❤️‍🔥"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "热", Emoticons: []string{"🔥"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "鼓掌", Emoticons: []string{"👏", "🎉"}, Version: 1, Enabled: true},
	{LangCode: "zh", Keyword: "恭喜", Emoticons: []string{"🎉", "👏", "🥳"}, Version: 1, Enabled: true},
	{LangCode: "en", Keyword: "laugh", Emoticons: []string{"😂", "🤣"}, Version: 1, Enabled: true},
	{LangCode: "en", Keyword: "lol", Emoticons: []string{"😂", "🤣"}, Version: 1, Enabled: true},
	{LangCode: "en", Keyword: "love", Emoticons: []string{"❤️", "😍"}, Version: 1, Enabled: true},
	{LangCode: "en", Keyword: "heart", Emoticons: []string{"❤️", "💖"}, Version: 1, Enabled: true},
	{LangCode: "en", Keyword: "fire", Emoticons: []string{"🔥", "❤️‍🔥"}, Version: 1, Enabled: true},
	{LangCode: "en", Keyword: "clap", Emoticons: []string{"👏", "🎉"}, Version: 1, Enabled: true},
	{LangCode: "en", Keyword: "party", Emoticons: []string{"🎉", "🥳"}, Version: 1, Enabled: true},
}

func New(ctx context.Context, dsn string) (*Service, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, nil
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(10 * time.Minute)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(10)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	svc := &Service{db: db}
	if err := svc.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return svc, nil
}

func (s *Service) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Service) EnsureSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS available_reactions (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			reaction VARCHAR(64) NOT NULL,
			title VARCHAR(128) NOT NULL,
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			premium_only TINYINT(1) NOT NULL DEFAULT 0,
			sort_order INT NOT NULL DEFAULT 0,
			static_icon_document_id BIGINT NOT NULL DEFAULT 0,
			appear_animation_document_id BIGINT NOT NULL DEFAULT 0,
			select_animation_document_id BIGINT NOT NULL DEFAULT 0,
			activate_animation_document_id BIGINT NOT NULL DEFAULT 0,
			effect_animation_document_id BIGINT NOT NULL DEFAULT 0,
			around_animation_document_id BIGINT NOT NULL DEFAULT 0,
			center_icon_document_id BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_available_reactions_reaction (reaction),
			KEY idx_available_reactions_enabled_order (enabled, sort_order)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
		`CREATE TABLE IF NOT EXISTS emoji_keywords (
			lang_code VARCHAR(16) NOT NULL,
			keyword VARCHAR(128) NOT NULL,
			emoji JSON NOT NULL,
			version INT NOT NULL DEFAULT 1,
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (lang_code, keyword),
			KEY idx_emoji_keywords_lang_version (lang_code, version, enabled)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure reaction schema: %w", err)
		}
	}
	for _, reaction := range defaultReactions {
		if err := s.seedReaction(ctx, reaction); err != nil {
			return err
		}
	}
	for _, keyword := range defaultKeywords {
		if err := s.seedKeyword(ctx, keyword); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListReactions(ctx context.Context) ([]Reaction, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, reaction, title, enabled, premium_only, sort_order,
		       static_icon_document_id, appear_animation_document_id, select_animation_document_id,
		       activate_animation_document_id, effect_animation_document_id, around_animation_document_id,
		       center_icon_document_id, created_at, updated_at
		FROM available_reactions ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reaction
	for rows.Next() {
		item, err := scanReaction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) UpsertReaction(ctx context.Context, reaction Reaction) (*Reaction, error) {
	reaction.Reaction = normalizeEmoji(reaction.Reaction)
	reaction.Title = strings.TrimSpace(reaction.Title)
	if reaction.Reaction == "" {
		return nil, errors.New("reaction is required")
	}
	if reaction.Title == "" {
		reaction.Title = reaction.Reaction
	}
	if reaction.SortOrder <= 0 {
		reaction.SortOrder = 100
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO available_reactions (
			reaction, title, enabled, premium_only, sort_order,
			static_icon_document_id, appear_animation_document_id, select_animation_document_id,
			activate_animation_document_id, effect_animation_document_id, around_animation_document_id, center_icon_document_id,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON DUPLICATE KEY UPDATE title = VALUES(title), enabled = VALUES(enabled), premium_only = VALUES(premium_only),
			sort_order = VALUES(sort_order), static_icon_document_id = VALUES(static_icon_document_id),
			appear_animation_document_id = VALUES(appear_animation_document_id),
			select_animation_document_id = VALUES(select_animation_document_id),
			activate_animation_document_id = VALUES(activate_animation_document_id),
			effect_animation_document_id = VALUES(effect_animation_document_id),
			around_animation_document_id = VALUES(around_animation_document_id),
			center_icon_document_id = VALUES(center_icon_document_id),
			updated_at = CURRENT_TIMESTAMP`,
		reaction.Reaction, reaction.Title, reaction.Enabled, reaction.PremiumOnly, reaction.SortOrder,
		reaction.StaticIconDocumentID, reaction.AppearAnimationDocumentID, reaction.SelectAnimationDocumentID,
		reaction.ActivateAnimationDocumentID, reaction.EffectAnimationDocumentID, reaction.AroundAnimationDocumentID, reaction.CenterIconDocumentID); err != nil {
		return nil, err
	}
	return s.getReactionByValue(ctx, reaction.Reaction)
}

func (s *Service) PatchReaction(ctx context.Context, id int64, patch ReactionPatch) (*Reaction, error) {
	reaction, err := s.getReactionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Title != nil {
		reaction.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Enabled != nil {
		reaction.Enabled = *patch.Enabled
	}
	if patch.PremiumOnly != nil {
		reaction.PremiumOnly = *patch.PremiumOnly
	}
	if patch.SortOrder != nil {
		reaction.SortOrder = *patch.SortOrder
	}
	if patch.StaticIconDocumentID != nil {
		reaction.StaticIconDocumentID = *patch.StaticIconDocumentID
	}
	if patch.AppearAnimationDocumentID != nil {
		reaction.AppearAnimationDocumentID = *patch.AppearAnimationDocumentID
	}
	if patch.SelectAnimationDocumentID != nil {
		reaction.SelectAnimationDocumentID = *patch.SelectAnimationDocumentID
	}
	if patch.ActivateAnimationDocumentID != nil {
		reaction.ActivateAnimationDocumentID = *patch.ActivateAnimationDocumentID
	}
	if patch.EffectAnimationDocumentID != nil {
		reaction.EffectAnimationDocumentID = *patch.EffectAnimationDocumentID
	}
	if patch.AroundAnimationDocumentID != nil {
		reaction.AroundAnimationDocumentID = *patch.AroundAnimationDocumentID
	}
	if patch.CenterIconDocumentID != nil {
		reaction.CenterIconDocumentID = *patch.CenterIconDocumentID
	}
	if reaction.Title == "" {
		reaction.Title = reaction.Reaction
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE available_reactions
		SET title = ?, enabled = ?, premium_only = ?, sort_order = ?,
		    static_icon_document_id = ?, appear_animation_document_id = ?, select_animation_document_id = ?,
		    activate_animation_document_id = ?, effect_animation_document_id = ?, around_animation_document_id = ?,
		    center_icon_document_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		reaction.Title, reaction.Enabled, reaction.PremiumOnly, reaction.SortOrder,
		reaction.StaticIconDocumentID, reaction.AppearAnimationDocumentID, reaction.SelectAnimationDocumentID,
		reaction.ActivateAnimationDocumentID, reaction.EffectAnimationDocumentID, reaction.AroundAnimationDocumentID,
		reaction.CenterIconDocumentID, id)
	if err != nil {
		return nil, err
	}
	updated, err := s.getReactionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *Service) ListEmojiKeywords(ctx context.Context, langCode string) ([]EmojiKeyword, error) {
	langCode = normalizeLang(langCode)
	rows, err := s.db.QueryContext(ctx, `
		SELECT lang_code, keyword, emoji, version, enabled, updated_at
		FROM emoji_keywords WHERE (? = '' OR lang_code = ?)
		ORDER BY lang_code ASC, keyword ASC`, langCode, langCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EmojiKeyword
	for rows.Next() {
		item, err := scanKeyword(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) UpsertEmojiKeyword(ctx context.Context, keyword EmojiKeyword) (*EmojiKeyword, error) {
	keyword.LangCode = normalizeLang(keyword.LangCode)
	keyword.Keyword = strings.TrimSpace(strings.ToLower(keyword.Keyword))
	if keyword.LangCode == "" || keyword.Keyword == "" || len(keyword.Emoticons) == 0 {
		return nil, errors.New("langCode, keyword and emoticons are required")
	}
	if keyword.Version <= 0 {
		keyword.Version = 1
	}
	data, _ := json.Marshal(keyword.Emoticons)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO emoji_keywords (lang_code, keyword, emoji, version, enabled, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON DUPLICATE KEY UPDATE emoji = VALUES(emoji), version = VALUES(version), enabled = VALUES(enabled), updated_at = CURRENT_TIMESTAMP`,
		keyword.LangCode, keyword.Keyword, string(data), keyword.Version, keyword.Enabled); err != nil {
		return nil, err
	}
	return s.getKeyword(ctx, keyword.LangCode, keyword.Keyword)
}

func (s *Service) seedReaction(ctx context.Context, reaction Reaction) error {
	reaction.Reaction = normalizeEmoji(reaction.Reaction)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO available_reactions (reaction, title, enabled, premium_only, sort_order, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON DUPLICATE KEY UPDATE title = VALUES(title), premium_only = VALUES(premium_only), sort_order = VALUES(sort_order)`,
		reaction.Reaction, reaction.Title, reaction.Enabled, reaction.PremiumOnly, reaction.SortOrder)
	return err
}

func (s *Service) seedKeyword(ctx context.Context, keyword EmojiKeyword) error {
	data, _ := json.Marshal(keyword.Emoticons)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO emoji_keywords (lang_code, keyword, emoji, version, enabled, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON DUPLICATE KEY UPDATE emoji = VALUES(emoji), version = VALUES(version), updated_at = CURRENT_TIMESTAMP`,
		normalizeLang(keyword.LangCode), strings.TrimSpace(strings.ToLower(keyword.Keyword)), string(data), keyword.Version, keyword.Enabled)
	return err
}

func (s *Service) getReactionByID(ctx context.Context, id int64) (Reaction, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, reaction, title, enabled, premium_only, sort_order,
		       static_icon_document_id, appear_animation_document_id, select_animation_document_id,
		       activate_animation_document_id, effect_animation_document_id, around_animation_document_id,
		       center_icon_document_id, created_at, updated_at
		FROM available_reactions WHERE id = ? LIMIT 1`, id)
	return scanReaction(row)
}

func (s *Service) getReactionByValue(ctx context.Context, value string) (*Reaction, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, reaction, title, enabled, premium_only, sort_order,
		       static_icon_document_id, appear_animation_document_id, select_animation_document_id,
		       activate_animation_document_id, effect_animation_document_id, around_animation_document_id,
		       center_icon_document_id, created_at, updated_at
		FROM available_reactions WHERE reaction = ? LIMIT 1`, value)
	item, err := scanReaction(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) getKeyword(ctx context.Context, langCode, keyword string) (*EmojiKeyword, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT lang_code, keyword, emoji, version, enabled, updated_at
		FROM emoji_keywords WHERE lang_code = ? AND keyword = ? LIMIT 1`, langCode, keyword)
	item, err := scanKeyword(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type reactionScanner interface {
	Scan(dest ...any) error
}

func scanReaction(row reactionScanner) (Reaction, error) {
	var item Reaction
	err := row.Scan(
		&item.ID, &item.Reaction, &item.Title, &item.Enabled, &item.PremiumOnly, &item.SortOrder,
		&item.StaticIconDocumentID, &item.AppearAnimationDocumentID, &item.SelectAnimationDocumentID,
		&item.ActivateAnimationDocumentID, &item.EffectAnimationDocumentID, &item.AroundAnimationDocumentID,
		&item.CenterIconDocumentID, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func scanKeyword(row reactionScanner) (EmojiKeyword, error) {
	var raw string
	var item EmojiKeyword
	if err := row.Scan(&item.LangCode, &item.Keyword, &raw, &item.Version, &item.Enabled, &item.UpdatedAt); err != nil {
		return item, err
	}
	_ = json.Unmarshal([]byte(raw), &item.Emoticons)
	return item, nil
}

func normalizeEmoji(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\ufe0e", ""))
	if value == "❤" {
		return "❤️"
	}
	return value
}

func normalizeLang(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "zh_cn", "zh-cn", "zh_hans", "zh-hans", "cn":
		return "zh"
	default:
		return value
	}
}
