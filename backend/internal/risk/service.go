package risk

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/teamgram/teamgram-server/pkg/riskctrl"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

type SignupIPStat struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
}

type Service struct {
	kv kv.Store
}

func New(store kv.Store) *Service {
	if store == nil {
		return nil
	}
	return &Service{kv: store}
}

func (s *Service) GetSettings(ctx context.Context) (riskctrl.Settings, error) {
	if s == nil || s.kv == nil {
		return riskctrl.DefaultSettings(), nil
	}
	v, err := s.kv.GetCtx(ctx, riskctrl.SettingsKey())
	if err != nil {
		return riskctrl.DefaultSettings(), err
	}
	if v == "" {
		return riskctrl.DefaultSettings(), nil
	}

	settings := riskctrl.DefaultSettings()
	if err := json.Unmarshal([]byte(v), &settings); err != nil {
		return riskctrl.DefaultSettings(), nil
	}
	return settings.Normalized(), nil
}

func (s *Service) UpdateSettings(ctx context.Context, settings riskctrl.Settings) (riskctrl.Settings, error) {
	settings = settings.Normalized()
	if s == nil || s.kv == nil {
		return settings, nil
	}
	b, _ := json.Marshal(settings)
	return settings, s.kv.SetCtx(ctx, riskctrl.SettingsKey(), string(b))
}

func (s *Service) SetKickLoginBlock(ctx context.Context, userID int64, duration time.Duration) error {
	if s == nil || s.kv == nil {
		return nil
	}
	seconds := int(duration.Seconds())
	if seconds <= 0 {
		_, err := s.kv.DelCtx(ctx, riskctrl.KickLoginBlockKey(userID))
		return err
	}
	until := strconv.FormatInt(time.Now().Add(duration).Unix(), 10)
	return s.kv.SetexCtx(ctx, riskctrl.KickLoginBlockKey(userID), until, seconds)
}

func (s *Service) ClearKickLoginBlock(ctx context.Context, userID int64) error {
	if s == nil || s.kv == nil {
		return nil
	}
	_, err := s.kv.DelCtx(ctx, riskctrl.KickLoginBlockKey(userID))
	return err
}

func (s *Service) GetTodaySignupStats(ctx context.Context, limit int) ([]SignupIPStat, error) {
	if s == nil || s.kv == nil {
		return nil, nil
	}
	data, err := s.kv.HgetallCtx(ctx, riskctrl.SignupIPDailyKey(time.Now()))
	if err != nil {
		return nil, err
	}
	stats := make([]SignupIPStat, 0, len(data))
	for ip, raw := range data {
		count, _ := strconv.Atoi(raw)
		stats = append(stats, SignupIPStat{IP: ip, Count: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Count == stats[j].Count {
			return stats[i].IP < stats[j].IP
		}
		return stats[i].Count > stats[j].Count
	})
	if limit > 0 && len(stats) > limit {
		stats = stats[:limit]
	}
	return stats, nil
}
