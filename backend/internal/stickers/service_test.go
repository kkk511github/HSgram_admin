package stickers

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestImportTelegramSetSuccessWritesStorageAndMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	storage := &recordingStorage{}
	svc := NewForTest(db, fakeTelegramClient{data: validWebP()}, storage)
	svc.random64 = func() int64 { return 1234 }

	expectImportSQL(mock, 950000000001, 960000000001, 8001, false)

	result, err := svc.ImportTelegramSet(context.Background(), ImportRequest{
		ShortName:              "licensed_set",
		AuthorizationStatement: "owned by our team",
		AdminID:                7,
	})
	if err != nil {
		t.Fatalf("ImportTelegramSet: %v", err)
	}
	if result.SetID != 950000000001 || result.StickerCount != 1 || result.Updated {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(storage.keys) != 1 || storage.keys[0] != "960000000001.dat" {
		t.Fatalf("storage was not written with document key: %#v", storage.keys)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportTelegramSetDuplicateUpdatesExistingSet(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	svc := NewForTest(db, fakeTelegramClient{data: validWebP()}, &recordingStorage{})
	svc.random64 = func() int64 { return 1234 }

	expectImportSQL(mock, 950000000001, 960000000001, 8001, true)

	result, err := svc.ImportTelegramSet(context.Background(), ImportRequest{
		ShortName:              "https://t.me/addstickers/licensed_set",
		AuthorizationStatement: "licensed import",
		AdminID:                7,
	})
	if err != nil {
		t.Fatalf("ImportTelegramSet duplicate: %v", err)
	}
	if !result.Updated {
		t.Fatalf("expected duplicate import to report update")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportTelegramSetDownloadFailureDoesNotWriteStorage(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	storage := &recordingStorage{}
	svc := NewForTest(db, fakeTelegramClient{downloadErr: errors.New("download failed")}, storage)

	_, err = svc.ImportTelegramSet(context.Background(), ImportRequest{
		ShortName:              "licensed_set",
		AuthorizationStatement: "licensed import",
		AdminID:                7,
	})
	if err == nil {
		t.Fatalf("expected download failure")
	}
	if len(storage.keys) != 0 {
		t.Fatalf("storage must not be written on download failure: %#v", storage.keys)
	}
}

func TestImportTelegramSetRejectsUnsupportedFormat(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	svc := NewForTest(db, fakeTelegramClient{data: []byte("not a sticker")}, &recordingStorage{})

	_, err = svc.ImportTelegramSet(context.Background(), ImportRequest{
		ShortName:              "licensed_set",
		AuthorizationStatement: "licensed import",
		AdminID:                7,
	})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("expected unsupported format, got %v", err)
	}
}

func TestImportTelegramSetCleansStorageOnMetadataFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	storage := &recordingStorage{}
	svc := NewForTest(db, fakeTelegramClient{data: validWebP()}, storage)
	svc.random64 = func() int64 { return 1234 }

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO sticker_sets").WillReturnResult(sqlmock.NewResult(950000000001, 1))
	mock.ExpectQuery("SELECT id, access_hash FROM sticker_sets").WillReturnRows(sqlmock.NewRows([]string{"id", "access_hash"}).AddRow(950000000001, 8001))
	mock.ExpectExec("DELETE rs FROM recent_stickers").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM stickers").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO stickers").WillReturnResult(sqlmock.NewResult(960000000001, 1))
	mock.ExpectExec("UPDATE stickers SET storage_key").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO documents").WillReturnError(errors.New("metadata failed"))
	mock.ExpectRollback()

	_, err = svc.ImportTelegramSet(context.Background(), ImportRequest{
		ShortName:              "licensed_set",
		AuthorizationStatement: "licensed import",
		AdminID:                7,
	})
	if err == nil {
		t.Fatalf("expected metadata failure")
	}
	if len(storage.keys) != 1 || len(storage.deleted) != 1 || storage.deleted[0] != storage.keys[0] {
		t.Fatalf("expected uploaded storage key to be cleaned, written=%#v deleted=%#v", storage.keys, storage.deleted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestImportTelegramSetRequiresAuthorizationStatement(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	svc := NewForTest(db, fakeTelegramClient{}, &recordingStorage{})
	_, err = svc.ImportTelegramSet(context.Background(), ImportRequest{ShortName: "licensed_set"})
	if !errors.Is(err, ErrAuthorizationNeeded) {
		t.Fatalf("expected authorization error, got %v", err)
	}
}

func TestNormalizeShortNameAcceptsStickerAndEmojiLinks(t *testing.T) {
	tests := map[string]string{
		"licensed_set":                          "licensed_set",
		"https://t.me/addstickers/licensed_set": "licensed_set",
		"http://t.me/addstickers/licensed_set/": "licensed_set",
		"t.me/addstickers/licensed_set":         "licensed_set",
		"https://t.me/addemoji/CreepyEmoji":     "creepyemoji",
		"http://t.me/addemoji/CreepyEmoji/":     "creepyemoji",
		"t.me/addemoji/CreepyEmoji":             "creepyemoji",
	}
	for input, want := range tests {
		if got := normalizeShortName(input); got != want {
			t.Fatalf("normalizeShortName(%q) = %q, want %q", input, got, want)
		}
	}
}

func expectImportSQL(mock sqlmock.Sqlmock, setID, stickerID, setAccessHash int64, duplicate bool) {
	mock.ExpectBegin()
	resultSetID := setID
	if duplicate {
		resultSetID = 0
	}
	mock.ExpectExec("INSERT INTO sticker_sets").WillReturnResult(sqlmock.NewResult(resultSetID, 1))
	mock.ExpectQuery("SELECT id, access_hash FROM sticker_sets").WillReturnRows(sqlmock.NewRows([]string{"id", "access_hash"}).AddRow(setID, setAccessHash))
	mock.ExpectExec("DELETE rs FROM recent_stickers").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM stickers").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO stickers").WillReturnResult(sqlmock.NewResult(stickerID, 1))
	mock.ExpectExec("UPDATE stickers SET storage_key").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO documents").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO sticker_emoji_index").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

type fakeTelegramClient struct {
	data        []byte
	downloadErr error
}

func (c fakeTelegramClient) GetStickerSet(ctx context.Context, shortName string) (TelegramStickerSet, error) {
	return TelegramStickerSet{
		Name:        normalizeShortName(shortName),
		Title:       "Licensed Set",
		StickerType: "regular",
		Stickers: []TelegramSticker{{
			FileID:       "file-1",
			FileUniqueID: "unique-1",
			Type:         "regular",
			Emoji:        "🙂",
			Width:        512,
			Height:       512,
			Keywords:     []string{"smile"},
		}},
	}, nil
}

func (c fakeTelegramClient) GetFile(ctx context.Context, fileID string) (TelegramFile, error) {
	return TelegramFile{FileID: fileID, FilePath: "stickers/file-1.webp"}, nil
}

func (c fakeTelegramClient) Download(ctx context.Context, filePath string) ([]byte, error) {
	if c.downloadErr != nil {
		return nil, c.downloadErr
	}
	return c.data, nil
}

type recordingStorage struct {
	keys    []string
	deleted []string
}

func (s *recordingStorage) PutDocument(ctx context.Context, key string, data []byte, contentType string) error {
	s.keys = append(s.keys, key)
	return nil
}

func (s *recordingStorage) DeleteDocument(ctx context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func validWebP() []byte {
	return []byte{'R', 'I', 'F', 'F', 1, 0, 0, 0, 'W', 'E', 'B', 'P', 'd', 'a', 't', 'a'}
}
