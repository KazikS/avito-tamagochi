package rewards_test

// Интеграционные тесты слоя данных, package rewards_test (внешний), как
// pet/repo_test.go и social/repo_test.go — те же причины. Питомец заводится
// SQL напрямую: этот пакет только ЧИТАЕТ pets, корректность его заполнения
// проверяет internal/pet.
//
// Без TEST_DATABASE_URL пропускаются; в CI переменная выставлена.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tamagochi/internal/config"
	"tamagochi/internal/rewards"
	"tamagochi/pkg/pgtest"
)

var base = time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)

type fixture struct {
	pool *pgxpool.Pool
	repo *rewards.Repo
	svc  *rewards.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := pgtest.Pool(t)
	repo := rewards.NewRepo(pool)
	svc, err := rewards.NewService(repo, rewards.DefaultCatalog, config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	return &fixture{pool: pool, repo: repo, svc: svc}
}

// seedPet заводит питомца с заданным суммарным опытом — единственное, от
// чего зависит уровень и, значит, eligibility.
func (f *fixture) seedPet(t *testing.T, userID uuid.UUID, totalXP int) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO pets (id, user_id, preset_id, name, hunger, joy, clean, energy, sleeping, stats_at, total_xp)
		VALUES ($1, $2, 'green', 'Тестовый', 100, 100, 100, 100, false, $3, $4)`,
		uuid.New(), userID, base, totalXP,
	)
	if err != nil {
		t.Fatalf("сидирование питомца: %v", err)
	}
}

// welcomeBadge — первая награда каталога (RequiredLevel=1): любой питомец с
// totalXP>=0 уже на 1 уровне, значит eligible сразу — самый простой фикстур
// для тестов, которым не важен конкретный порог.
const welcomeBadge = "welcome-badge"

// boostListing — вторая награда каталога (RequiredLevel=3, DefaultCurve:
// 100 XP на 1→2, 130 на 2→3) — нужен для тестов на «недостаточно уровня».
const boostListing = "boost-listing"

func TestClaimGrantsWhenEligible(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0)

	result, err := f.svc.Claim(ctx, userID, welcomeBadge, uuid.New())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if result.Replayed {
		t.Error("Replayed = true на первом claim")
	}
	if result.Reward.Status != rewards.StatusClaimed {
		t.Errorf("Status = %q, ожидался claimed", result.Reward.Status)
	}
	if result.Reward.ClaimedAt == nil {
		t.Error("ClaimedAt = nil после успешного claim")
	}
}

func TestClaimRejectsWhenNotEligible(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0) // уровень 1, boostListing требует 3

	_, err := f.svc.Claim(ctx, userID, boostListing, uuid.New())
	if !errors.Is(err, rewards.ErrNotEligible) {
		t.Fatalf("Claim = %v, ожидалась ErrNotEligible", err)
	}
}

func TestClaimWithoutPetIsRejected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	_, err := f.svc.Claim(ctx, uuid.New(), welcomeBadge, uuid.New())
	if !errors.Is(err, rewards.ErrNoPet) {
		t.Fatalf("Claim = %v, ожидалась ErrNoPet", err)
	}
}

func TestClaimUnknownRewardIsRejected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0)

	_, err := f.svc.Claim(ctx, userID, "не-существует", uuid.New())
	if !errors.Is(err, rewards.ErrUnknownReward) {
		t.Fatalf("Claim = %v, ожидалась ErrUnknownReward", err)
	}
}

// Повтор с ТЕМ ЖЕ Idempotency-Key — легитимный ретрай, обязан вернуть
// прежний результат, а не ошибку и не второй грант.
func TestClaimIsIdempotentWithSameKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0)
	key := uuid.New()

	first, err := f.svc.Claim(ctx, userID, welcomeBadge, key)
	if err != nil {
		t.Fatalf("первый claim: %v", err)
	}
	second, err := f.svc.Claim(ctx, userID, welcomeBadge, key)
	if err != nil {
		t.Fatalf("повторный claim тем же ключом: %v", err)
	}
	if !second.Replayed {
		t.Error("Replayed = false на повторе тем же ключом")
	}
	if !first.Reward.ClaimedAt.Equal(*second.Reward.ClaimedAt) {
		t.Errorf("ClaimedAt разошёлся между повторами: %v != %v", first.Reward.ClaimedAt, second.Reward.ClaimedAt)
	}
}

// Повтор ДРУГИМ Idempotency-Key по уже полученной награде — не ретрай,
// а вторая попытка получить то, что уже выдано (контракт: 409 REWARD_ALREADY_CLAIMED).
func TestClaimWithDifferentKeyIsRejected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0)

	if _, err := f.svc.Claim(ctx, userID, welcomeBadge, uuid.New()); err != nil {
		t.Fatalf("первый claim: %v", err)
	}
	_, err := f.svc.Claim(ctx, userID, welcomeBadge, uuid.New())
	if !errors.Is(err, rewards.ErrAlreadyClaimed) {
		t.Fatalf("Claim другим ключом = %v, ожидалась ErrAlreadyClaimed", err)
	}
}

// raceRoundSameKey запускает racers горутин, ВСЕ бьющие claim одной и той же
// награды одним и тем же Idempotency-Key, отпущенных одним закрытием start —
// без общего барьера горутины стартуют с разбросом в микросекунды, которого
// достаточно, чтобы первая уже закоммитилась до того, как вторая дойдёт до
// своего первого запроса, и гонка ни разу не случится (проверено: без
// барьера этот тест ни разу не поймал намеренно убранные FOR UPDATE и
// UNIQUE-constraint одновременно). Тот же приём, что raceRound в
// internal/pet/repo_test.go.
func raceRoundSameKey(t *testing.T, svc *rewards.Service, userID uuid.UUID, key uuid.UUID, racers int) {
	t.Helper()
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([]error, racers)
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := svc.Claim(ctx, userID, welcomeBadge, key)
			results[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Errorf("горутина %d: Claim вернул ошибку %v, ожидался успех (тот же ключ — легитимный ретрай)", i, err)
		}
	}
}

// invariant:4 — однократная выдача награды при конкурентных claim'ах.
// Буквальнее, чем одноимённый маркер в internal/pet/repo_test.go (тот про
// идемпотентность действия ухода, не про Reward-домен, которого на момент
// его написания ещё не было) — здесь настоящий reward_grants.
//
// Инвариант пакета (AGENTS.md рядом → Всегда): N параллельных claim'ов
// одной награды одним ключом → ровно одна запись в reward_grants. Против
// настоящего Postgres, с -race, не против мока — мок не докажет гонку.
//
// Несколько раундов на разных пользователях, не один: гонка либо
// случается в узком окне между «гранта ещё нет» и записью, либо нет —
// один раунд может повезти и не задеть окно вообще (см. docstring
// raceRoundSameKey и уже задокументированный опыт internal/pet: 8 горутин
// без раундов ловили намеренную поломку лишь в 45% прогонов).
func TestConcurrentClaimGrantsExactlyOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	const rounds = 5
	for round := range rounds {
		t.Run(fmt.Sprintf("раунд_%d", round+1), func(t *testing.T) {
			userID := uuid.New()
			f.seedPet(t, userID, 0)
			raceRoundSameKey(t, f.svc, userID, uuid.New(), 16)

			var count int
			if err := f.pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM reward_grants WHERE user_id = $1 AND reward_id = $2`,
				userID, welcomeBadge,
			).Scan(&count); err != nil {
				t.Fatalf("подсчёт грантов: %v", err)
			}
			if count != 1 {
				t.Fatalf("reward_grants содержит %d строк для (user, reward), ожидалась ровно 1 — грант задвоился", count)
			}
		})
	}
}

// Второй инвариант гонки: N горутин с РАЗНЫМИ ключами — выигрывает ровно
// одна, остальные получают ErrAlreadyClaimed, ни одна не проходит тихо.
// Барьер и раунды — по той же причине, что в TestConcurrentClaimGrantsExactlyOnce.
func TestConcurrentClaimWithDistinctKeysGrantsExactlyOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	const rounds = 5
	for round := range rounds {
		t.Run(fmt.Sprintf("раунд_%d", round+1), func(t *testing.T) {
			userID := uuid.New()
			f.seedPet(t, userID, 0)

			const n = 16
			var wg sync.WaitGroup
			results := make([]error, n)
			start := make(chan struct{})
			for i := range n {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					_, err := f.svc.Claim(ctx, userID, welcomeBadge, uuid.New())
					results[i] = err
				}(i)
			}
			close(start)
			wg.Wait()

			successes, conflicts := 0, 0
			for i, err := range results {
				switch {
				case err == nil:
					successes++
				case errors.Is(err, rewards.ErrAlreadyClaimed):
					conflicts++
				default:
					t.Errorf("горутина %d: %v, ожидался успех или ErrAlreadyClaimed", i, err)
				}
			}
			if successes != 1 {
				t.Errorf("успешных claim'ов: %d, ожидался ровно 1 (остальные %d — конфликт)", successes, conflicts)
			}

			var count int
			if err := f.pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM reward_grants WHERE user_id = $1 AND reward_id = $2`,
				userID, welcomeBadge,
			).Scan(&count); err != nil {
				t.Fatalf("подсчёт грантов: %v", err)
			}
			if count != 1 {
				t.Fatalf("reward_grants содержит %d строк, ожидалась ровно 1", count)
			}
		})
	}
}

func TestRedeemMarksUsed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0)

	if _, err := f.svc.Claim(ctx, userID, welcomeBadge, uuid.New()); err != nil {
		t.Fatalf("claim: %v", err)
	}
	reward, err := f.svc.Redeem(ctx, userID, welcomeBadge)
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if reward.Status != rewards.StatusUsed {
		t.Errorf("Status = %q, ожидался used", reward.Status)
	}
	if reward.UsedAt == nil {
		t.Error("UsedAt = nil после Redeem")
	}
}

func TestRedeemWithoutClaimIsRejected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0)

	_, err := f.svc.Redeem(ctx, userID, welcomeBadge)
	if !errors.Is(err, rewards.ErrNotClaimed) {
		t.Fatalf("Redeem без claim = %v, ожидалась ErrNotClaimed", err)
	}
}

// POST .../redeem-click идемпотентен: повторный клик по уже применённой
// награде — не ошибка (контракт отвечает 200 в обоих случаях).
func TestRedeemIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0)

	if _, err := f.svc.Claim(ctx, userID, welcomeBadge, uuid.New()); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := f.svc.Redeem(ctx, userID, welcomeBadge); err != nil {
		t.Fatalf("первый redeem: %v", err)
	}
	if _, err := f.svc.Redeem(ctx, userID, welcomeBadge); err != nil {
		t.Fatalf("повторный redeem: %v", err)
	}
}

// Список отражает и locked/available (по уровню), и claimed/used (по
// гранту) — не только «получено/не получено».
func TestListReflectsProgressAndStatus(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0) // уровень 1: welcomeBadge(1) available/claimed, boostListing(3) locked

	if _, err := f.svc.Claim(ctx, userID, welcomeBadge, uuid.New()); err != nil {
		t.Fatalf("claim: %v", err)
	}

	list, err := f.svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]rewards.Reward{}
	for _, r := range list {
		byID[r.ID] = r
	}
	if byID[welcomeBadge].Status != rewards.StatusClaimed {
		t.Errorf("welcomeBadge.Status = %q, ожидался claimed", byID[welcomeBadge].Status)
	}
	if byID[boostListing].Status != rewards.StatusLocked {
		t.Errorf("boostListing.Status = %q, ожидался locked (уровень 1 < 3)", byID[boostListing].Status)
	}
}

// Next выбирает ближайшую ПО ПРОГРЕССУ (наименьший требуемый уровень) среди
// ещё не полученных, а не первую в каталоге и не последнюю полученную.
func TestNextReturnsNearestUnclaimed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, 0)

	if _, err := f.svc.Claim(ctx, userID, welcomeBadge, uuid.New()); err != nil {
		t.Fatalf("claim welcomeBadge: %v", err)
	}

	next, found, err := f.svc.Next(ctx, userID)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !found {
		t.Fatal("Next: found=false, хотя в каталоге есть ещё не полученные награды")
	}
	if next.ID != boostListing {
		t.Errorf("Next.ID = %q, ожидался %q (наименьший RequiredLevel среди неполученных)", next.ID, boostListing)
	}
}
