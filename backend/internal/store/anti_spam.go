package store

import (
	"context"
	"strings"
	"time"
)

type AntiSpamFalsePositiveFilter struct {
	ChannelID      int64
	ReporterUserID int64
	Limit          int
	Offset         int
}

type AntiSpamFalsePositiveRecord struct {
	ChannelID      int64     `json:"channelId"`
	ReporterUserID int64     `json:"reporterUserId"`
	MsgID          int32     `json:"msgId"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type AntiSpamFalsePositiveList struct {
	Items  []AntiSpamFalsePositiveRecord `json:"items"`
	Limit  int                           `json:"limit"`
	Offset int                           `json:"offset"`
}

func (s *Store) ListAntiSpamFalsePositives(ctx context.Context, filter AntiSpamFalsePositiveFilter) (AntiSpamFalsePositiveList, error) {
	limit := filter.Limit
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where := make([]string, 0, 2)
	args := make([]any, 0, 4)
	if filter.ChannelID > 0 {
		where = append(where, "channel_id = ?")
		args = append(args, filter.ChannelID)
	}
	if filter.ReporterUserID > 0 {
		where = append(where, "reporter_user_id = ?")
		args = append(args, filter.ReporterUserID)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT channel_id, reporter_user_id, msg_id, created_at, updated_at
		FROM channel_anti_spam_false_positives`+whereClause+`
		ORDER BY updated_at DESC, channel_id DESC, reporter_user_id DESC, msg_id DESC
		LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return AntiSpamFalsePositiveList{}, err
	}
	defer rows.Close()

	items := make([]AntiSpamFalsePositiveRecord, 0)
	for rows.Next() {
		var item AntiSpamFalsePositiveRecord
		if err := rows.Scan(
			&item.ChannelID,
			&item.ReporterUserID,
			&item.MsgID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return AntiSpamFalsePositiveList{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AntiSpamFalsePositiveList{}, err
	}

	return AntiSpamFalsePositiveList{
		Items:  items,
		Limit:  limit,
		Offset: offset,
	}, nil
}
