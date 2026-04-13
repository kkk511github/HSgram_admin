package broadcast

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hsgram-admin/backend/internal/messenger"
	"hsgram-admin/backend/internal/store"
)

type Service struct {
	store  *store.Store
	sender *messenger.Client
}

type Request struct {
	MessageText string                 `json:"messageText"`
	TargetType  string                 `json:"targetType"`
	Identifiers string                 `json:"identifiers"`
	Filters     store.BroadcastFilters `json:"filters"`
}

func New(userStore *store.Store, sender *messenger.Client) *Service {
	return &Service{
		store:  userStore,
		sender: sender,
	}
}

func (s *Service) Preview(ctx context.Context, req Request) (store.BroadcastPreview, store.BroadcastTargetSpec, error) {
	spec, unresolved, err := s.buildTargetSpec(ctx, req)
	if err != nil {
		return store.BroadcastPreview{}, store.BroadcastTargetSpec{}, err
	}

	preview, err := s.store.PreviewBroadcastTargets(ctx, spec)
	if err != nil {
		return store.BroadcastPreview{}, store.BroadcastTargetSpec{}, err
	}
	preview.Unresolved = unresolved
	return preview, spec, nil
}

func (s *Service) Enqueue(ctx context.Context, admin store.AdminUser, req Request) (*store.BroadcastRecord, store.BroadcastPreview, error) {
	messageText := strings.TrimSpace(req.MessageText)
	if messageText == "" {
		return nil, store.BroadcastPreview{}, fmt.Errorf("message text is required")
	}

	preview, spec, err := s.Preview(ctx, req)
	if err != nil {
		return nil, store.BroadcastPreview{}, err
	}
	if preview.Count == 0 {
		return nil, store.BroadcastPreview{}, fmt.Errorf("no broadcast recipients found")
	}

	record, err := s.store.CreateBroadcast(ctx, admin, messageText, spec, preview.Count)
	if err != nil {
		return nil, store.BroadcastPreview{}, err
	}
	return record, preview, nil
}

func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}

	_ = s.store.ResetRunningBroadcasts(ctx)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				processed, err := s.processNext(ctx)
				if err != nil || !processed {
					break
				}
			}
		}
	}
}

func (s *Service) buildTargetSpec(ctx context.Context, req Request) (store.BroadcastTargetSpec, []string, error) {
	spec := store.BroadcastTargetSpec{
		Type: strings.TrimSpace(req.TargetType),
	}

	switch spec.Type {
	case store.BroadcastTargetTypeAll:
		return spec, nil, nil
	case store.BroadcastTargetTypeSelected:
		recipients, unresolved, err := s.store.ResolveSelectedRecipients(ctx, req.Identifiers)
		if err != nil {
			return store.BroadcastTargetSpec{}, nil, err
		}
		if len(recipients) == 0 {
			return store.BroadcastTargetSpec{}, unresolved, fmt.Errorf("no valid selected users found")
		}

		spec.SourceValues = storeSplitIdentifiers(req.Identifiers)
		spec.ResolvedUserIDs = make([]int64, 0, len(recipients))
		for _, recipient := range recipients {
			spec.ResolvedUserIDs = append(spec.ResolvedUserIDs, recipient.UserID)
		}
		return spec, unresolved, nil
	case store.BroadcastTargetTypeFiltered:
		spec.Filters = req.Filters
		return spec, nil, nil
	default:
		return store.BroadcastTargetSpec{}, nil, fmt.Errorf("unsupported target type")
	}
}

func (s *Service) processNext(ctx context.Context) (bool, error) {
	record, err := s.store.ClaimNextBroadcast(ctx)
	if err != nil {
		return false, err
	}
	if record == nil {
		return false, nil
	}

	var spec store.BroadcastTargetSpec
	if err := json.Unmarshal(record.TargetSpec, &spec); err != nil {
		_ = s.store.MarkBroadcastFailed(ctx, record.ID, 0, 0, fmt.Sprintf("invalid target spec: %v", err))
		return true, nil
	}

	totalTargets, err := s.store.CountRecipients(ctx, spec)
	if err != nil {
		_ = s.store.MarkBroadcastFailed(ctx, record.ID, 0, 0, err.Error())
		return true, nil
	}
	if totalTargets == 0 {
		_ = s.store.MarkBroadcastFailed(ctx, record.ID, 0, 0, "no recipients found")
		return true, nil
	}

	var successCount int
	var failureCount int
	offset := 0
	const batchSize = 200

	for {
		recipients, err := s.store.ListRecipients(ctx, spec, offset, batchSize)
		if err != nil {
			_ = s.store.MarkBroadcastFailed(ctx, record.ID, successCount, failureCount, err.Error())
			return true, nil
		}
		if len(recipients) == 0 {
			break
		}

		for _, recipient := range recipients {
			err := s.sender.SendTextMessage(ctx, store.BroadcastSystemUserID, recipient.UserID, record.MessageText)
			if err != nil {
				failureCount++
				_ = s.store.UpsertBroadcastDelivery(ctx, record.ID, recipient, store.BroadcastDeliveryFailed, err.Error(), false)
			} else {
				successCount++
				_ = s.store.UpsertBroadcastDelivery(ctx, record.ID, recipient, store.BroadcastDeliverySent, "", true)
			}
		}

		_ = s.store.UpdateBroadcastProgress(ctx, record.ID, successCount, failureCount)
		offset += len(recipients)
	}

	if successCount == 0 && failureCount > 0 {
		_ = s.store.MarkBroadcastFailed(ctx, record.ID, successCount, failureCount, "all deliveries failed")
		return true, nil
	}

	_ = s.store.MarkBroadcastCompleted(ctx, record.ID, successCount, failureCount)
	return true, nil
}

func storeSplitIdentifiers(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '\n', '\r', '\t', ';', ' ':
			return true
		default:
			return false
		}
	})
}
