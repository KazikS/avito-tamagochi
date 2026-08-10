// Package rewards — награды. Тег контракта "rewards".
//
// Награда — энтайтлмент, не промокод (docs/DECISIONS.md → 10.08): право,
// привязанное к user_id через reward_grants (UNIQUE(user_id, reward_id)).
// Кода, который можно переслать, в ответе нет вообще — см. AGENTS.md рядом
// («Никогда: не возвращай награду как пересылаемую строку-код»).
//
// Каталог — провизорные числа (уровни, тексты), как DefaultEconomy в
// internal/pet: замена одним литералом, когда экономика утрясётся. Только
// cosmetic и soft: premium в контракте существует, но требует вехи
// 100-дневного стрика, которого в проекте нет — выдумывать недостижимую
// награду нечестнее, чем не показывать её (docs/DECISIONS.md → 10.08).
package rewards

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"tamagochi/internal/config"
)

// Tier — класс награды.
type Tier string

const (
	// TierCosmetic — себестоимость 0, выдаётся напрямую.
	TierCosmetic Tier = "cosmetic"
	// TierSoft — бонусы кошелька, мелкие скидки на услуги Авито.
	TierSoft Tier = "soft"
	// TierPremium существует в перечислении контракта, каталог его не
	// выдаёт (см. докстринг пакета).
	TierPremium Tier = "premium"
)

// Status — статус награды для конкретного пользователя.
type Status string

const (
	StatusLocked    Status = "locked"
	StatusAvailable Status = "available"
	StatusClaimed   Status = "claimed"
	StatusUsed      Status = "used"
)

// CatalogEntry — статическое описание награды.
type CatalogEntry struct {
	ID            string
	Tier          Tier
	Title         string
	Description   string
	RequiredLevel int
	// CosmeticID — непусто, если Tier == TierCosmetic.
	CosmeticID string
}

// DefaultCatalog — набор наград MVP. Уровни отражают темп DefaultCurve
// (internal/config): не выше того, что реалистично достижимо за время
// демо-показа с учётом суточного капа опыта.
//
// Названия и типы наград — реальные категории услуг Авито (поднятие,
// доставка, продвижение), не выдуманные бренды: связь с продуктом — прямое
// требование кейса («награды - мотивировать делать основные действия,
// которые предоставляет доска объявлений»), и ровно то, что уже раздаёт
// «Портал призов» (docs/DECISIONS.md → 10.08).
var DefaultCatalog = []CatalogEntry{
	{
		ID: "welcome-badge", Tier: TierCosmetic, RequiredLevel: 1,
		Title:       "Значок «Первые шаги»",
		Description: "Украшение для Ави — открывается сразу, с первым уровнем.",
		CosmeticID:  "badge-welcome",
	},
	{
		ID: "boost-listing", Tier: TierSoft, RequiredLevel: 3,
		Title:       "Бесплатное поднятие объявления",
		Description: "Разовое поднятие одного объявления в топ категории.",
	},
	{
		ID: "delivery-discount", Tier: TierSoft, RequiredLevel: 6,
		Title:       "Скидка на Авито Доставку",
		Description: "Скидка на следующую отправку через Авито Доставку.",
	},
	{
		ID: "promotion-week", Tier: TierSoft, RequiredLevel: 10,
		Title:       "Неделя продвижения объявления",
		Description: "Семь дней приоритетного показа одного объявления.",
	},
}

// Reward — награда, как её видит клиент: каталог, смешанный с прогрессом
// конкретного пользователя. Без JSON-тегов контракта — маппинг делает
// handler.go, ровно как Stats/View в internal/pet.
type Reward struct {
	ID              string
	Tier            Tier
	Title           string
	Description     string
	ConditionLabel  string
	ProgressCurrent int
	ProgressTarget  int
	Status          Status
	CosmeticID      string
	ClaimedAt       *time.Time
	UsedAt          *time.Time
}

// toReward переводит запись каталога в Reward для конкретного уровня
// пользователя и (если есть) записи гранта.
func toReward(entry CatalogEntry, level int, grant *Grant) Reward {
	status := StatusLocked
	if level >= entry.RequiredLevel {
		status = StatusAvailable
	}
	current := level
	if current > entry.RequiredLevel {
		current = entry.RequiredLevel
	}

	out := Reward{
		ID:              entry.ID,
		Tier:            entry.Tier,
		Title:           entry.Title,
		Description:     entry.Description,
		ConditionLabel:  fmt.Sprintf("Уровень %d", entry.RequiredLevel),
		ProgressCurrent: current,
		ProgressTarget:  entry.RequiredLevel,
		Status:          status,
		CosmeticID:      entry.CosmeticID,
	}
	if grant != nil {
		out.Status = StatusClaimed
		grantedAt := grant.GrantedAt
		out.ClaimedAt = &grantedAt
		if grant.UsedAt != nil {
			out.Status = StatusUsed
			usedAt := *grant.UsedAt
			out.UsedAt = &usedAt
		}
	}
	return out
}

// Сентинелы claim: handler.go решает по ним HTTP-код, не по тексту ошибки.
var (
	// ErrUnknownReward — rewardId не входит в каталог.
	ErrUnknownReward = errors.New("rewards: неизвестная награда")
	// ErrNoPet — у пользователя нет питомца, значит нет и уровня.
	ErrNoPet = errors.New("rewards: нет питомца")
	// ErrNotEligible — уровень пользователя ниже требуемого.
	ErrNotEligible = errors.New("rewards: условие награды не выполнено")
	// ErrAlreadyClaimed — повторный claim другим Idempotency-Key.
	ErrAlreadyClaimed = errors.New("rewards: награда уже выдана")
)

// Service собирает сценарии наград.
type Service struct {
	repo    *Repo
	catalog []CatalogEntry
	curve   config.Curve
}

// NewService собирает сервис. Как pet.NewService и social.NewService,
// отказывается собираться на бессмысленных константах — падать на старте,
// не на первом запросе.
func NewService(repo *Repo, catalog []CatalogEntry, curve config.Curve) (*Service, error) {
	switch {
	case repo == nil:
		return nil, errors.New("rewards: нет репозитория")
	case len(catalog) == 0:
		return nil, errors.New("rewards: пустой каталог")
	}
	if err := curve.Validate(); err != nil {
		return nil, err
	}
	return &Service{repo: repo, catalog: catalog, curve: curve}, nil
}

func (s *Service) findEntry(rewardID string) (CatalogEntry, bool) {
	for _, e := range s.catalog {
		if e.ID == rewardID {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

// levelOf — уровень пользователя по totalXP питомца, 0 без питомца
// (ProgressCurrent=0, все награды locked).
func (s *Service) levelOf(totalXP int, hasPet bool) (int, error) {
	if !hasPet {
		return 0, nil
	}
	progress, err := config.LevelFor(totalXP, s.curve)
	if err != nil {
		return 0, fmt.Errorf("rewards: уровень по опыту %d: %w", totalXP, err)
	}
	return progress.Level, nil
}

// List возвращает весь каталог с прогрессом пользователя.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Reward, error) {
	totalXP, hasPet, err := s.repo.PetTotalXP(ctx, userID)
	if err != nil {
		return nil, err
	}
	level, err := s.levelOf(totalXP, hasPet)
	if err != nil {
		return nil, err
	}

	grants, err := s.repo.GrantsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	byReward := make(map[string]Grant, len(grants))
	for _, g := range grants {
		byReward[g.RewardID] = g
	}

	out := make([]Reward, 0, len(s.catalog))
	for _, entry := range s.catalog {
		var grantPtr *Grant
		if g, ok := byReward[entry.ID]; ok {
			grantPtr = &g
		}
		out = append(out, toReward(entry, level, grantPtr))
	}
	return out, nil
}

// Next возвращает ближайшую ещё не полученную награду по прогрессу —
// наименьший RequiredLevel среди locked/available. found=false, если все
// награды каталога уже получены.
func (s *Service) Next(ctx context.Context, userID uuid.UUID) (reward Reward, found bool, err error) {
	rewards, err := s.List(ctx, userID)
	if err != nil {
		return Reward{}, false, err
	}
	var best *Reward
	for i := range rewards {
		r := &rewards[i]
		if r.Status == StatusClaimed || r.Status == StatusUsed {
			continue
		}
		if best == nil || r.ProgressTarget < best.ProgressTarget {
			best = r
		}
	}
	if best == nil {
		return Reward{}, false, nil
	}
	return *best, true, nil
}

// ClaimResult — результат Claim.
type ClaimResult struct {
	Reward   Reward
	Replayed bool
}

// Claim выдаёт награду — единственный сценарий, который пишет в
// reward_grants (через Repo.Claim). Идемпотентно по (userID, rewardID,
// idempotencyKey): см. докстринг Repo.Claim и AGENTS.md рядом.
func (s *Service) Claim(ctx context.Context, userID uuid.UUID, rewardID string, idempotencyKey uuid.UUID) (ClaimResult, error) {
	entry, ok := s.findEntry(rewardID)
	if !ok {
		return ClaimResult{}, fmt.Errorf("%w: %q", ErrUnknownReward, rewardID)
	}

	grant, totalXP, replayed, err := s.repo.Claim(ctx, userID, rewardID, idempotencyKey, func(totalXP int, hasPet bool) error {
		if !hasPet {
			return ErrNoPet
		}
		level, lvlErr := s.levelOf(totalXP, hasPet)
		if lvlErr != nil {
			return lvlErr
		}
		if level < entry.RequiredLevel {
			return ErrNotEligible
		}
		return nil
	})
	switch {
	case errors.Is(err, ErrKeyMismatch):
		return ClaimResult{}, ErrAlreadyClaimed
	case err != nil:
		return ClaimResult{}, err
	}

	level, err := s.levelOf(totalXP, true)
	if err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{Reward: toReward(entry, level, &grant), Replayed: replayed}, nil
}

// Redeem отмечает награду применённой — POST /rewards/{rewardId}/redeem-click.
// MVP-упрощение (docs/DECISIONS.md → 10.08): в полном продукте статус
// перешёл бы в used отдельным серверным событием со стороны Авито, здесь —
// синхронно, интеграции с настоящим Авито нет.
func (s *Service) Redeem(ctx context.Context, userID uuid.UUID, rewardID string) (Reward, error) {
	entry, ok := s.findEntry(rewardID)
	if !ok {
		return Reward{}, fmt.Errorf("%w: %q", ErrUnknownReward, rewardID)
	}
	if err := s.repo.MarkUsed(ctx, userID, rewardID); err != nil {
		return Reward{}, err
	}

	totalXP, hasPet, err := s.repo.PetTotalXP(ctx, userID)
	if err != nil {
		return Reward{}, err
	}
	level, err := s.levelOf(totalXP, hasPet)
	if err != nil {
		return Reward{}, err
	}
	grants, err := s.repo.GrantsByUser(ctx, userID)
	if err != nil {
		return Reward{}, err
	}
	for _, g := range grants {
		if g.RewardID == rewardID {
			return toReward(entry, level, &g), nil
		}
	}
	// MarkUsed уже проверил существование гранта — сюда попасть нельзя.
	return Reward{}, fmt.Errorf("rewards: грант %s/%s исчез между применением и чтением", userID, rewardID)
}
