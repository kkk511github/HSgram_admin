package stickers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/jsonx"
)

const (
	SourcePlatformTelegram = "telegram"
	MaxStickerFileSize     = 8 << 20
)

var (
	ErrImporterUnavailable = errors.New("sticker importer unavailable")
	ErrAuthorizationNeeded = errors.New("authorization statement is required")
	ErrUnsupportedFormat   = errors.New("unsupported sticker format")
)

type Config struct {
	DatabaseDSN      string
	TelegramBotToken string
	Storage          Storage
	HTTPClient       *http.Client
}

type Service struct {
	db       *sql.DB
	client   TelegramClient
	storage  Storage
	ownsDB   bool
	now      func() time.Time
	random64 func() int64
}

type ImportRequest struct {
	ShortName              string
	Source                 string
	AuthorizationStatement string
	AdminID                int64
}

type ImportResult struct {
	SetID        int64  `json:"setId"`
	ShortName    string `json:"shortName"`
	Title        string `json:"title"`
	StickerCount int    `json:"stickerCount"`
	Hash         int64  `json:"hash"`
	Updated      bool   `json:"updated"`
}

type SetSummary struct {
	ID             int64      `json:"id"`
	ShortName      string     `json:"shortName"`
	Title          string     `json:"title"`
	StickerType    string     `json:"stickerType"`
	Source         string     `json:"source"`
	SourcePlatform string     `json:"sourcePlatform"`
	Hash           int64      `json:"hash"`
	StickerCount   int        `json:"stickerCount"`
	DisabledAt     *time.Time `json:"disabledAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type StickerSummary struct {
	ID       int64    `json:"id"`
	Emoji    string   `json:"emoji"`
	Format   string   `json:"format"`
	MimeType string   `json:"mimeType"`
	Width    int32    `json:"width"`
	Height   int32    `json:"height"`
	Size     int64    `json:"size"`
	SHA256   string   `json:"sha256"`
	Position int32    `json:"position"`
	Keywords []string `json:"keywords,omitempty"`
}

type SetDetail struct {
	SetSummary
	Stickers []StickerSummary `json:"stickers"`
}

type TelegramClient interface {
	GetStickerSet(ctx context.Context, shortName string) (TelegramStickerSet, error)
	GetFile(ctx context.Context, fileID string) (TelegramFile, error)
	Download(ctx context.Context, filePath string) ([]byte, error)
}

type Storage interface {
	PutDocument(ctx context.Context, key string, data []byte, contentType string) error
	DeleteDocument(ctx context.Context, key string) error
}

type TelegramStickerSet struct {
	Name        string
	Title       string
	StickerType string
	Stickers    []TelegramSticker
}

type TelegramSticker struct {
	FileID       string
	FileUniqueID string
	Type         string
	Emoji        string
	Width        int32
	Height       int32
	FileSize     int64
	Keywords     []string
}

type TelegramFile struct {
	FileID   string
	FilePath string
	FileSize int64
}

func New(ctx context.Context, cfg Config) (*Service, error) {
	var db *sql.DB
	if strings.TrimSpace(cfg.DatabaseDSN) != "" {
		var err error
		db, err = sql.Open("mysql", cfg.DatabaseDSN)
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
	}
	svc := &Service{
		db:       db,
		client:   NewBotAPIClient(cfg.TelegramBotToken, cfg.HTTPClient),
		storage:  cfg.Storage,
		ownsDB:   true,
		now:      func() time.Time { return time.Now().UTC() },
		random64: randomInt64,
	}
	if db != nil {
		if err := svc.EnsureSchema(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return svc, nil
}

func NewForTest(db *sql.DB, client TelegramClient, storage Storage) *Service {
	return &Service{
		db:       db,
		client:   client,
		storage:  storage,
		now:      func() time.Time { return time.Now().UTC() },
		random64: randomInt64,
	}
}

func (s *Service) Close() error {
	if s != nil && s.ownsDB && s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Service) ImportTelegramSet(ctx context.Context, req ImportRequest) (*ImportResult, error) {
	if s == nil || s.db == nil || s.client == nil || s.storage == nil {
		return nil, ErrImporterUnavailable
	}
	shortName := normalizeShortName(req.ShortName)
	if shortName == "" {
		return nil, fmt.Errorf("shortName is required")
	}
	if strings.TrimSpace(req.AuthorizationStatement) == "" {
		return nil, ErrAuthorizationNeeded
	}
	set, err := s.client.GetStickerSet(ctx, shortName)
	if err != nil {
		return nil, fmt.Errorf("telegram getStickerSet: %w", err)
	}
	if len(set.Stickers) == 0 {
		return nil, fmt.Errorf("telegram sticker set is empty")
	}

	downloaded := make([]importSticker, 0, len(set.Stickers))
	for i, st := range set.Stickers {
		file, err := s.client.GetFile(ctx, st.FileID)
		if err != nil {
			return nil, fmt.Errorf("telegram getFile %s: %w", st.FileID, err)
		}
		data, err := s.client.Download(ctx, file.FilePath)
		if err != nil {
			return nil, fmt.Errorf("download sticker %s: %w", st.FileID, err)
		}
		item, err := validateImportedSticker(st, file, data, int32(i))
		if err != nil {
			return nil, err
		}
		downloaded = append(downloaded, item)
	}
	return s.persistImport(ctx, req, set, downloaded)
}

func (s *Service) ListSets(ctx context.Context) ([]SetSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.short_name, s.title, s.sticker_type, s.source, s.source_platform, s.hash,
		       COUNT(st.id), s.disabled_at, s.created_at, s.updated_at
		FROM sticker_sets s
		LEFT JOIN stickers st ON st.set_id = s.id
		GROUP BY s.id
		ORDER BY s.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sets []SetSummary
	for rows.Next() {
		item, err := scanSetSummary(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, item)
	}
	return sets, rows.Err()
}

func (s *Service) GetSet(ctx context.Context, id int64) (*SetDetail, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.short_name, s.title, s.sticker_type, s.source, s.source_platform, s.hash,
		       COUNT(st.id), s.disabled_at, s.created_at, s.updated_at
		FROM sticker_sets s
		LEFT JOIN stickers st ON st.set_id = s.id
		WHERE s.id = ?
		GROUP BY s.id`, id)
	summary, err := scanSetSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stickers, err := s.listStickers(ctx, id)
	if err != nil {
		return nil, err
	}
	return &SetDetail{SetSummary: summary, Stickers: stickers}, nil
}

func (s *Service) DisableSet(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sticker_sets SET disabled_at = CURRENT_TIMESTAMP WHERE id = ? AND disabled_at IS NULL`, id)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type importSticker struct {
	TelegramSticker
	Data       []byte
	MimeType   string
	Format     string
	SHA256     string
	Position   int32
	StorageKey string
	AccessHash int64
}

func validateImportedSticker(st TelegramSticker, file TelegramFile, data []byte, position int32) (importSticker, error) {
	if len(data) == 0 {
		return importSticker{}, fmt.Errorf("sticker %s is empty", st.FileID)
	}
	if len(data) > MaxStickerFileSize {
		return importSticker{}, fmt.Errorf("sticker %s exceeds max size", st.FileID)
	}
	format, mimeType, err := detectStickerFormat(st, file.FilePath, data)
	if err != nil {
		return importSticker{}, err
	}
	sum := sha256.Sum256(data)
	return importSticker{
		TelegramSticker: st,
		Data:            data,
		MimeType:        mimeType,
		Format:          format,
		SHA256:          hex.EncodeToString(sum[:]),
		Position:        position,
	}, nil
}

func detectStickerFormat(st TelegramSticker, path string, data []byte) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case bytes.HasPrefix(data, []byte("RIFF")) && len(data) >= 12 && string(data[8:12]) == "WEBP":
		return "static", "image/webp", nil
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}):
		return "static", "image/png", nil
	case strings.EqualFold(ext, ".tgs") || bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		return "animated", "application/x-tgsticker", nil
	case strings.EqualFold(ext, ".webm") || bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return "video", "video/webm", nil
	default:
		return "", "", fmt.Errorf("%w: %s", ErrUnsupportedFormat, st.FileID)
	}
}

func (s *Service) persistImport(ctx context.Context, req ImportRequest, set TelegramStickerSet, stickers []importSticker) (*ImportResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var (
		uploadedKeys []string
		committed    bool
	)
	defer func() {
		if committed {
			return
		}
		for _, key := range uploadedKeys {
			_ = s.storage.DeleteDocument(ctx, key)
		}
	}()

	source := strings.TrimSpace(req.AuthorizationStatement)
	if extra := strings.TrimSpace(req.Source); extra != "" {
		source = extra + " | " + source
	}
	setAccessHash := s.random64()
	hash := importHash(set, stickers)
	res, err := tx.ExecContext(ctx, `
		INSERT INTO sticker_sets (access_hash, short_name, title, sticker_type, source, source_platform, hash, created_by, disabled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON DUPLICATE KEY UPDATE
			title = VALUES(title),
			sticker_type = VALUES(sticker_type),
			source = VALUES(source),
			source_platform = VALUES(source_platform),
			hash = VALUES(hash),
			created_by = VALUES(created_by),
			disabled_at = NULL,
			updated_at = CURRENT_TIMESTAMP`,
		setAccessHash,
		normalizeShortName(set.Name),
		set.Title,
		normalizeStickerType(set.StickerType),
		source,
		SourcePlatformTelegram,
		hash,
		req.AdminID,
	)
	if err != nil {
		return nil, err
	}
	lastID, _ := res.LastInsertId()
	updated := lastID == 0
	var (
		setID                  int64
		persistedSetAccessHash int64
	)
	if err := tx.QueryRowContext(ctx, `SELECT id, access_hash FROM sticker_sets WHERE short_name = ?`, normalizeShortName(set.Name)).Scan(&setID, &persistedSetAccessHash); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE rs FROM recent_stickers rs
		INNER JOIN stickers st ON st.id = rs.sticker_id
		WHERE st.set_id = ?`, setID); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM stickers WHERE set_id = ?`, setID); err != nil {
		return nil, err
	}
	for _, st := range stickers {
		st.AccessHash = accessHashForMime(st.MimeType, s.random64())
		res, err := tx.ExecContext(ctx, `
			INSERT INTO stickers (
				set_id, access_hash, emoji, format, mime_type, width, height, size, storage_key, thumb_storage_key,
				sha256, position, keywords, telegram_file_id, telegram_file_unique_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?, ?, ?)`,
			setID,
			st.AccessHash,
			st.Emoji,
			st.Format,
			st.MimeType,
			st.Width,
			st.Height,
			int64(len(st.Data)),
			st.SHA256,
			st.Position,
			keywordsJSON(st.Keywords),
			st.FileID,
			st.FileUniqueID,
		)
		if err != nil {
			return nil, err
		}
		stickerID, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d.dat", stickerID)
		if err := s.storage.PutDocument(ctx, key, st.Data, st.MimeType); err != nil {
			return nil, err
		}
		uploadedKeys = append(uploadedKeys, key)
		if _, err := tx.ExecContext(ctx, `UPDATE stickers SET storage_key = ? WHERE id = ?`, key, stickerID); err != nil {
			return nil, err
		}
		if err := insertDocumentRow(ctx, tx, stickerID, st.AccessHash, key, setID, persistedSetAccessHash, st); err != nil {
			return nil, err
		}
		if strings.TrimSpace(st.Emoji) != "" {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO sticker_emoji_index (emoji, sticker_id, set_id, weight, source)
				VALUES (?, ?, ?, 100, 'telegram_import')
				ON DUPLICATE KEY UPDATE set_id = VALUES(set_id), weight = VALUES(weight), source = VALUES(source)`,
				strings.TrimSpace(st.Emoji), stickerID, setID); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return &ImportResult{
		SetID:        setID,
		ShortName:    normalizeShortName(set.Name),
		Title:        set.Title,
		StickerCount: len(stickers),
		Hash:         hash,
		Updated:      updated,
	}, nil
}

func insertDocumentRow(ctx context.Context, tx *sql.Tx, stickerID, accessHash int64, key string, setID, setAccessHash int64, st importSticker) error {
	inputSet := mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: setID, AccessHash: setAccessHash}).To_InputStickerSet()
	displayName := stickerDisplayFileName(stickerID, st.MimeType)
	attrs := []*mtproto.DocumentAttribute{
		mtproto.MakeTLDocumentAttributeImageSize(&mtproto.DocumentAttribute{W: st.Width, H: st.Height}).To_DocumentAttribute(),
		mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{Alt: st.Emoji, Stickerset: inputSet}).To_DocumentAttribute(),
		mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: displayName}).To_DocumentAttribute(),
	}
	attrJSON, _ := jsonx.Marshal(attrs)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO documents (document_id, access_hash, dc_id, file_path, file_size, uploaded_file_name, ext, mime_type, thumb_id, video_thumb_id, attributes, date2)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?, 0, 0, ?, ?)
		ON DUPLICATE KEY UPDATE
			access_hash = VALUES(access_hash),
			file_path = VALUES(file_path),
			file_size = VALUES(file_size),
			uploaded_file_name = VALUES(uploaded_file_name),
			ext = VALUES(ext),
			mime_type = VALUES(mime_type),
			attributes = VALUES(attributes),
			date2 = VALUES(date2)`,
		stickerID,
		accessHash,
		key,
		int64(len(st.Data)),
		displayName,
		fileExtForMime(st.MimeType),
		st.MimeType,
		string(attrJSON),
		time.Now().Unix(),
	)
	return err
}

func (s *Service) listStickers(ctx context.Context, setID int64) ([]StickerSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, emoji, format, mime_type, width, height, size, sha256, position, keywords
		FROM stickers WHERE set_id = ? ORDER BY position ASC, id ASC`, setID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []StickerSummary
	for rows.Next() {
		var item StickerSummary
		var keywords sql.NullString
		if err := rows.Scan(&item.ID, &item.Emoji, &item.Format, &item.MimeType, &item.Width, &item.Height, &item.Size, &item.SHA256, &item.Position, &keywords); err != nil {
			return nil, err
		}
		if keywords.Valid && keywords.String != "" {
			_ = json.Unmarshal([]byte(keywords.String), &item.Keywords)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type setScanner interface {
	Scan(dest ...any) error
}

func scanSetSummary(row setScanner) (SetSummary, error) {
	var item SetSummary
	var disabled sql.NullTime
	err := row.Scan(&item.ID, &item.ShortName, &item.Title, &item.StickerType, &item.Source, &item.SourcePlatform, &item.Hash, &item.StickerCount, &disabled, &item.CreatedAt, &item.UpdatedAt)
	if disabled.Valid {
		item.DisabledAt = &disabled.Time
	}
	return item, err
}

func keywordsJSON(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(values)
	return string(data)
}

func importHash(set TelegramStickerSet, stickers []importSticker) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(normalizeShortName(set.Name)))
	_, _ = h.Write([]byte(set.Title))
	var buf [8]byte
	for _, st := range stickers {
		_, _ = h.Write([]byte(st.SHA256))
		binary.LittleEndian.PutUint64(buf[:], uint64(len(st.Data)))
		_, _ = h.Write(buf[:])
	}
	return int64(h.Sum64() & 0x7fffffff)
}

func normalizeShortName(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{
		"https://t.me/addstickers/",
		"http://t.me/addstickers/",
		"t.me/addstickers/",
		"https://t.me/addemoji/",
		"http://t.me/addemoji/",
		"t.me/addemoji/",
	} {
		value = strings.TrimPrefix(value, prefix)
	}
	return strings.ToLower(strings.Trim(value, "/"))
}

func normalizeStickerType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mask", "masks":
		return "mask"
	case "custom_emoji":
		return "custom_emoji"
	default:
		return "regular"
	}
}

func fileExtForMime(mimeType string) string {
	switch mimeType {
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	case "application/x-tgsticker":
		return ".tgs"
	case "video/webm":
		return ".webm"
	default:
		return ".dat"
	}
}

func stickerDisplayFileName(id int64, mimeType string) string {
	return fmt.Sprintf("sticker_%d%s", id, fileExtForMime(mimeType))
}

func accessHashForMime(mimeType string, randomPart int64) int64 {
	var fileType int32
	switch mimeType {
	case "image/webp":
		fileType = int32(mtproto.CRC32_storage_fileWebp)
	case "image/png":
		fileType = int32(mtproto.CRC32_storage_filePng)
	case "video/webm":
		fileType = int32(mtproto.CRC32_storage_fileMp4)
	default:
		fileType = int32(mtproto.CRC32_storage_fileUnknown)
	}
	return int64(fileType)<<32 | (randomPart & 0xffffffff)
}

func randomInt64() int64 {
	var buf [8]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(buf[:]) & 0x7fffffffffffffff)
}
