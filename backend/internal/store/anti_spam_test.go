package store

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestListAntiSpamFalsePositivesFiltersAndClamps(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	createdAt := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(5 * time.Minute)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT channel_id, reporter_user_id, msg_id, created_at, updated_at")).
		WithArgs(int64(1001), int64(42), 200, 0).
		WillReturnRows(sqlmock.NewRows([]string{"channel_id", "reporter_user_id", "msg_id", "created_at", "updated_at"}).
			AddRow(int64(1001), int64(42), int32(77), createdAt, updatedAt))

	store := &Store{db: db}
	result, err := store.ListAntiSpamFalsePositives(context.Background(), AntiSpamFalsePositiveFilter{
		ChannelID:      1001,
		ReporterUserID: 42,
		Limit:          500,
		Offset:         -10,
	})
	if err != nil {
		t.Fatalf("ListAntiSpamFalsePositives: %v", err)
	}
	if result.Limit != 200 || result.Offset != 0 {
		t.Fatalf("expected limit/offset to be clamped, got limit=%d offset=%d", result.Limit, result.Offset)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one item, got %d", len(result.Items))
	}
	item := result.Items[0]
	if item.ChannelID != 1001 || item.ReporterUserID != 42 || item.MsgID != 77 || !item.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected item: %#v", item)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
