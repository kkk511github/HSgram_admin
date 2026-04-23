package invitecodes

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/kv"
	redisstore "github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	codesHashKey     = "hs:signup_invite:codes"
	usesKeyPrefix    = "hs:signup_invite:uses"
	settingsCacheKey = "hs:signup_invite:settings:v1"
)

var (
	ErrCodeExists   = errors.New("invite code already exists")
	ErrCodeNotFound = errors.New("invite code not found")
	ErrCodeInvalid  = errors.New("invite code is invalid")
)

type Service struct {
	kv kv.Store
}

type Code struct {
	Code      string `json:"code"`
	Note      string `json:"note,omitempty"`
	MaxUses   int    `json:"maxUses"`
	UsedCount int    `json:"usedCount"`
	Enabled   bool   `json:"enabled"`
	CreatedBy string `json:"createdBy,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type Usage struct {
	UsageID string `json:"usageId"`
	Phone   string `json:"phone"`
	UserID  int64  `json:"userId"`
	Status  string `json:"status"`
	UsedAt  int64  `json:"usedAt"`
}

type Detail struct {
	Code   Code    `json:"code"`
	Usages []Usage `json:"usages"`
}

type CreateRequest struct {
	Code    string `json:"code"`
	Note    string `json:"note"`
	MaxUses int    `json:"maxUses"`
}

type Settings struct {
	Enabled   bool  `json:"enabled"`
	UpdatedAt int64 `json:"updatedAt"`
}

func New(store kv.Store) *Service {
	if store == nil {
		return nil
	}
	return &Service{kv: store}
}

func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func DefaultSettings() Settings {
	return Settings{
		Enabled:   false,
		UpdatedAt: 0,
	}
}

func (s *Service) ListCodes(ctx context.Context) ([]Code, error) {
	if s == nil || s.kv == nil {
		return nil, nil
	}

	rawMap, err := s.kv.HgetallCtx(ctx, codesHashKey)
	if err != nil {
		return nil, err
	}

	codes := make([]Code, 0, len(rawMap))
	for _, raw := range rawMap {
		var code Code
		if err := json.Unmarshal([]byte(raw), &code); err != nil {
			continue
		}
		codes = append(codes, code)
	}

	sort.Slice(codes, func(i, j int) bool {
		if codes[i].CreatedAt == codes[j].CreatedAt {
			return codes[i].Code < codes[j].Code
		}
		return codes[i].CreatedAt > codes[j].CreatedAt
	})
	return codes, nil
}

func (s *Service) CreateCode(ctx context.Context, actor string, req CreateRequest) (Code, error) {
	if s == nil || s.kv == nil {
		return Code{}, ErrCodeInvalid
	}

	code := NormalizeCode(req.Code)
	if code == "" {
		code = generateCode()
	}
	if !isCodeAllowed(code) {
		return Code{}, ErrCodeInvalid
	}
	if req.MaxUses < 0 {
		return Code{}, ErrCodeInvalid
	}

	now := time.Now().Unix()
	record := Code{
		Code:      code,
		Note:      strings.TrimSpace(req.Note),
		MaxUses:   req.MaxUses,
		UsedCount: 0,
		Enabled:   true,
		CreatedBy: strings.TrimSpace(actor),
		CreatedAt: now,
		UpdatedAt: now,
	}

	payload, _ := json.Marshal(&record)
	ok, err := s.kv.HsetnxCtx(ctx, codesHashKey, code, string(payload))
	if err != nil {
		return Code{}, err
	}
	if !ok {
		return Code{}, ErrCodeExists
	}

	return record, nil
}

func (s *Service) GetDetail(ctx context.Context, rawCode string) (*Detail, error) {
	if s == nil || s.kv == nil {
		return nil, ErrCodeNotFound
	}

	code, err := s.getCode(ctx, rawCode)
	if err != nil {
		return nil, err
	}

	usageMap, err := s.kv.HgetallCtx(ctx, usesKey(rawCode))
	if err != nil {
		return nil, err
	}

	usages := make([]Usage, 0, len(usageMap))
	for _, raw := range usageMap {
		var usage Usage
		if err := json.Unmarshal([]byte(raw), &usage); err != nil {
			continue
		}
		usages = append(usages, usage)
	}

	sort.Slice(usages, func(i, j int) bool {
		if usages[i].UsedAt == usages[j].UsedAt {
			return usages[i].UsageID < usages[j].UsageID
		}
		return usages[i].UsedAt > usages[j].UsedAt
	})

	return &Detail{
		Code:   *code,
		Usages: usages,
	}, nil
}

func (s *Service) SetCodeEnabled(ctx context.Context, rawCode string, enabled bool) (*Code, error) {
	if s == nil || s.kv == nil {
		return nil, ErrCodeNotFound
	}

	code, err := s.getCode(ctx, rawCode)
	if err != nil {
		return nil, err
	}

	code.Enabled = enabled
	code.UpdatedAt = time.Now().Unix()
	payload, _ := json.Marshal(code)
	if err := s.kv.HsetCtx(ctx, codesHashKey, code.Code, string(payload)); err != nil {
		return nil, err
	}
	return code, nil
}

func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	settings := DefaultSettings()
	if s == nil || s.kv == nil {
		return settings, nil
	}

	raw, err := s.kv.GetCtx(ctx, settingsCacheKey)
	if err != nil {
		if errors.Is(err, redisstore.Nil) {
			return settings, nil
		}
		return settings, err
	}
	if raw == "" {
		return settings, nil
	}

	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return DefaultSettings(), nil
	}
	return settings, nil
}

func (s *Service) UpdateSettings(ctx context.Context, enabled bool) (Settings, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return settings, err
	}

	settings.Enabled = enabled
	settings.UpdatedAt = time.Now().Unix()
	if s == nil || s.kv == nil {
		return settings, nil
	}

	payload, _ := json.Marshal(&settings)
	return settings, s.kv.SetCtx(ctx, settingsCacheKey, string(payload))
}

func (s *Service) getCode(ctx context.Context, rawCode string) (*Code, error) {
	code := NormalizeCode(rawCode)
	if code == "" {
		return nil, ErrCodeInvalid
	}

	raw, err := s.kv.HgetCtx(ctx, codesHashKey, code)
	if err != nil {
		if errors.Is(err, redisstore.Nil) {
			return nil, ErrCodeNotFound
		}
		return nil, err
	}
	if raw == "" {
		return nil, ErrCodeNotFound
	}

	var record Code
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func usesKey(rawCode string) string {
	return fmt.Sprintf("%s:%s", usesKeyPrefix, NormalizeCode(rawCode))
}

func generateCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("HS-%d", time.Now().Unix())
	}

	out := make([]byte, 0, 10)
	for i, b := range buf {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, alphabet[int(b)%len(alphabet)])
	}
	return string(out)
}

func isCodeAllowed(code string) bool {
	if len(code) < 4 || len(code) > 32 {
		return false
	}
	for _, ch := range code {
		switch {
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '-':
		default:
			return false
		}
	}
	return true
}
