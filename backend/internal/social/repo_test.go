package social_test

// Интеграционные тесты слоя данных. package social_test (внешний), как и
// internal/pet/repo_test.go — те же причины. Данные заводятся SQL напрямую,
// не через internal/pet.Service: этот пакет только ЧИТАЕТ pets и
// pet_action_log, корректность их заполнения проверяет internal/pet.
//
// Без TEST_DATABASE_URL пропускаются; в CI переменная выставлена.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tamagochi/internal/config"
	"tamagochi/internal/pet"
	"tamagochi/internal/social"
	"tamagochi/pkg/clock"
	"tamagochi/pkg/pgtest"
)

var base = time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)

type seedFixture struct {
	pool *pgxpool.Pool
	clk  *clock.Fixed
	svc  *social.Service
}

func newSeedFixture(t *testing.T) *seedFixture {
	t.Helper()
	pool := pgtest.Pool(t)
	clk := clock.NewFixed(base)
	svc, err := social.NewService(social.NewRepo(pool), clk, config.DefaultCurve)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}
	return &seedFixture{pool: pool, clk: clk, svc: svc}
}

// seedPet заводит питомца с заданным суммарным опытом.
func (f *seedFixture) seedPet(t *testing.T, userID uuid.UUID, name string, totalXP int) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO pets (id, user_id, preset_id, name, hunger, joy, clean, energy, sleeping, stats_at, total_xp)
		VALUES ($1, $2, 'green', $3, 100, 100, 100, 100, false, $4, $5)`,
		uuid.New(), userID, name, base, totalXP,
	)
	if err != nil {
		t.Fatalf("сидирование питомца %s: %v", name, err)
	}
}

// seedXP добавляет запись в журнал действий на указанный day (влияет на
// weekly_xp), не трогая total_xp питомца — для лидерборда важен только журнал.
func (f *seedFixture) seedXP(t *testing.T, userID uuid.UUID, day time.Time, xp int) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO pet_action_log (user_id, action_id, kind, xp_granted, day, result)
		VALUES ($1, $2, 'feed', $3, $4, '{}'::jsonb)`,
		userID, uuid.New(), xp, day,
	)
	if err != nil {
		t.Fatalf("сидирование опыта: %v", err)
	}
}

func day(offsetDays int) time.Time {
	y, m, d := base.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, offsetDays)
}

// ptr — адрес значения там, где сама функция (day(0)) его не даёт.
func ptr(t time.Time) *time.Time { return &t }

// seedAction сидирует запись журнала с конкретным kind/xp/created_at и
// произвольным result — через pet.ActResult, как это делает
// internal/pet.Service.Act, а не сырым JSON: см. докстринг repo.go → Day
// про то, почему регистр ключей внутри result важен.
func (f *seedFixture) seedAction(t *testing.T, userID uuid.UUID, kind string, xp int, day, createdAt time.Time, result pet.ActResult) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("сериализация результата действия: %v", err)
	}
	_, err = f.pool.Exec(context.Background(), `
		INSERT INTO pet_action_log (user_id, action_id, kind, xp_granted, day, result, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID, uuid.New(), kind, xp, day, raw, createdAt,
	)
	if err != nil {
		t.Fatalf("сидирование действия: %v", err)
	}
}

// --- Сводка дня ------------------------------------------------------

// Пустые сутки — ShouldShow=false, а не пустая сводка со всеми нулями,
// выглядящая как «уход был, но нулевой»: контракт явно отдаёт решение
// показывать сводку серверу.
func TestSummaryDayWithNoActionsShouldNotShow(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, "Тихоня", 0)

	got, err := f.svc.Summary(ctx, userID, ptr(day(0)))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.ShouldShow {
		t.Error("ShouldShow = true без единого действия за сутки")
	}
	if got.HasPetSnapshot {
		t.Error("HasPetSnapshot = true без единого действия за сутки — нечем его подтвердить")
	}
	if got.XPTotal != 0 {
		t.Errorf("XPTotal = %d, ожидался 0", got.XPTotal)
	}
	if got.LevelBefore != got.LevelAfter {
		t.Errorf("LevelBefore=%d != LevelAfter=%d — без опыта за сутки уровень не должен меняться", got.LevelBefore, got.LevelAfter)
	}
}

// Разбивка группируется по kind: два одинаковых действия — одна строка с
// count=2, а не две строки. XPTotal — сумма по всем строкам.
func TestSummaryDayAggregatesBreakdownByKind(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, "Едок", 0)

	f.seedAction(t, userID, "feed", 10, day(0), base, pet.ActResult{})
	f.seedAction(t, userID, "feed", 10, day(0), base.Add(time.Minute), pet.ActResult{})
	f.seedAction(t, userID, "play", 12, day(0), base.Add(2*time.Minute), pet.ActResult{})

	got, err := f.svc.Summary(ctx, userID, ptr(day(0)))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !got.ShouldShow {
		t.Fatal("ShouldShow = false при трёх действиях за сутки")
	}
	if got.XPTotal != 32 {
		t.Errorf("XPTotal = %d, ожидалось 32", got.XPTotal)
	}

	byKind := map[string]social.BreakdownEntry{}
	for _, b := range got.Breakdown {
		byKind[b.Key] = b
	}
	if got.Breakdown != nil && len(byKind) != len(got.Breakdown) {
		t.Fatalf("разбивка содержит дублирующиеся kind: %+v", got.Breakdown)
	}
	if byKind["feed"].Count != 2 || byKind["feed"].XP != 20 {
		t.Errorf("feed = %+v, ожидалось count=2 xp=20", byKind["feed"])
	}
	if byKind["play"].Count != 1 || byKind["play"].XP != 12 {
		t.Errorf("play = %+v, ожидалось count=1 xp=12", byKind["play"])
	}
	if byKind["feed"].Label != "Покормить" {
		t.Errorf("feed.Label = %q, ожидалось «Покормить»", byKind["feed"].Label)
	}
}

// levelBefore/levelAfter — из СУММАРНОГО опыта строго до суток и включительно
// по них, посчитанного той же config.LevelFor, что и everywhere else в
// проекте — не хардкод и не второй способ посчитать то же самое.
func TestSummaryDayLevelBeforeAfterFromCumulativeXP(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, "Растущий", 0)

	f.seedAction(t, userID, "feed", 100, day(-1), base.AddDate(0, 0, -1), pet.ActResult{})
	f.seedAction(t, userID, "feed", 130, day(0), base, pet.ActResult{})

	got, err := f.svc.Summary(ctx, userID, ptr(day(0)))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}

	wantBefore, err := config.LevelFor(100, config.DefaultCurve)
	if err != nil {
		t.Fatalf("LevelFor(100): %v", err)
	}
	wantAfter, err := config.LevelFor(230, config.DefaultCurve)
	if err != nil {
		t.Fatalf("LevelFor(230): %v", err)
	}
	if got.LevelBefore != wantBefore.Level {
		t.Errorf("LevelBefore = %d, ожидалось %d (по опыту 100 СТРОГО до суток)", got.LevelBefore, wantBefore.Level)
	}
	if got.LevelAfter != wantAfter.Level {
		t.Errorf("LevelAfter = %d, ожидалось %d (по опыту 230 включительно по сутки)", got.LevelAfter, wantAfter.Level)
	}
}

// StatsAfter/MoodAfter берутся из ПОСЛЕДНЕГО по времени действия суток, а не
// из первого и не из случайного порядка выборки.
func TestSummaryDayUsesLastActionOfDayForStatsAndMood(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, "Смотрящий", 0)

	early := pet.ActResult{Pet: pet.View{Stats: pet.Stats{Hunger: 40, Joy: 40, Clean: 40, Energy: 40}, Mood: pet.MoodSad}}
	late := pet.ActResult{Pet: pet.View{Stats: pet.Stats{Hunger: 90, Joy: 95, Clean: 100, Energy: 85}, Mood: pet.MoodHappy}}

	f.seedAction(t, userID, "feed", 10, day(0), base, early)
	f.seedAction(t, userID, "wash", 10, day(0), base.Add(time.Hour), late)

	got, err := f.svc.Summary(ctx, userID, ptr(day(0)))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !got.HasPetSnapshot {
		t.Fatal("HasPetSnapshot = false при двух действиях за сутки")
	}
	if got.StatsAfter != late.Pet.Stats {
		t.Errorf("StatsAfter = %+v, ожидалось %+v (последнее по времени действие)", got.StatsAfter, late.Pet.Stats)
	}
	if got.MoodAfter != pet.MoodHappy {
		t.Errorf("MoodAfter = %q, ожидалось %q — из последнего действия, не первого", got.MoodAfter, pet.MoodHappy)
	}
}

// Питомца никогда не было — сводка пустая, но это не ошибка: пользователю
// без питомца всё ещё должна открываться страница сводки.
func TestSummaryWithoutPetIsEmptyNotError(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()

	got, err := f.svc.Summary(ctx, uuid.New(), ptr(day(0)))
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.ShouldShow || got.HasPetSnapshot {
		t.Errorf("ShouldShow/HasPetSnapshot должны быть false без питомца: %+v", got)
	}
}

// date=nil — «сегодня» по часам сервиса, а не «за всё время»: действие
// завтрашним днём не должно попасть в сегодняшнюю сводку.
func TestSummaryNilDateDefaultsToToday(t *testing.T) {
	f := newSeedFixture(t) // часы фиксированы на base
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, "Сегодняшний", 0)
	f.seedAction(t, userID, "feed", 10, day(0), base, pet.ActResult{})
	f.seedAction(t, userID, "feed", 999, day(1), base.AddDate(0, 0, 1), pet.ActResult{})

	got, err := f.svc.Summary(ctx, userID, nil)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.XPTotal != 10 {
		t.Errorf("XPTotal = %d, ожидалось 10 — nil-дата должна взять «сегодня» по часам сервиса, не всё подряд", got.XPTotal)
	}
	if !got.Date.Equal(day(0)) {
		t.Errorf("Date = %v, ожидалось %v", got.Date, day(0))
	}
}

// MarkSeen — идемпотентна: повторная отметка тех же суток не должна ни
// упасть, ни задвоить строку (контракт отвечает 204 в обоих случаях).
func TestMarkSeenIsIdempotent(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	f.seedPet(t, userID, "Отмечающий", 0)

	if err := f.svc.MarkSeen(ctx, userID, day(0)); err != nil {
		t.Fatalf("первая отметка: %v", err)
	}
	if err := f.svc.MarkSeen(ctx, userID, day(0)); err != nil {
		t.Fatalf("повторная отметка того же дня: %v", err)
	}

	var seenCount int
	if err := f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM daily_summary_views WHERE user_id = $1 AND date = $2`,
		userID, day(0),
	).Scan(&seenCount); err != nil {
		t.Fatalf("проверка отметки: %v", err)
	}
	if seenCount != 1 {
		t.Errorf("daily_summary_views содержит %d строк, ожидалась 1 — повтор обязан быть идемпотентным, не задваивать", seenCount)
	}
}

// Базовый порядок: больше опыта за неделю — выше в списке.
func TestLeaderboardOrdersByWeeklyXP(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()

	low, mid, high := uuid.New(), uuid.New(), uuid.New()
	f.seedPet(t, low, "Low", 0)
	f.seedPet(t, mid, "Mid", 0)
	f.seedPet(t, high, "High", 0)
	f.seedXP(t, low, day(0), 10)
	f.seedXP(t, mid, day(0), 50)
	f.seedXP(t, high, day(0), 90)

	page, err := f.svc.List(ctx, high, "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("строк %d, ожидалось 3", len(page.Items))
	}
	wantOrder := []string{"High", "Mid", "Low"}
	for i, want := range wantOrder {
		if page.Items[i].Nickname != want {
			t.Errorf("позиция %d: %q, ожидалось %q", i, page.Items[i].Nickname, want)
		}
		if page.Items[i].Rank != i+1 {
			t.Errorf("позиция %d: rank=%d, ожидался %d", i, page.Items[i].Rank, i+1)
		}
	}
}

// invariant-смежное: опыт ЗА ПРЕДЕЛАМИ 7-дневного окна не считается —
// иначе лидерборд был бы рейтингом всех времён, а не «за неделю».
func TestLeaderboardIgnoresXPOutsideWindow(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()

	recent, stale := uuid.New(), uuid.New()
	f.seedPet(t, recent, "Recent", 0)
	f.seedPet(t, stale, "Stale", 0)
	f.seedXP(t, recent, day(-6), 20)  // ровно на границе окна — считается
	f.seedXP(t, stale, day(-7), 1000) // на день раньше границы — не считается

	page, err := f.svc.List(ctx, recent, "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	byName := map[string]int{}
	for _, e := range page.Items {
		byName[e.Nickname] = e.WeeklyXP
	}
	if byName["Recent"] != 20 {
		t.Errorf("Recent.weeklyXp = %d, ожидалось 20", byName["Recent"])
	}
	if byName["Stale"] != 0 {
		t.Errorf("Stale.weeklyXp = %d, ожидалось 0 — опыт за 8 дней назад не должен считаться", byName["Stale"])
	}
	if byName["Stale"] >= byName["Recent"] {
		t.Fatalf("Stale (вне окна) не должен обгонять Recent (в окне): %d >= %d", byName["Stale"], byName["Recent"])
	}
}

// Курсор — это ранг, не смещение: страницы обязаны идти встык без повторов
// и пропусков, даже когда лимит меньше числа участников.
func TestLeaderboardPaginationCoversEveryoneOnce(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()

	const n = 7
	ids := make([]uuid.UUID, n)
	for i := range n {
		ids[i] = uuid.New()
		f.seedPet(t, ids[i], uuid.New().String()[:8], 0)
		f.seedXP(t, ids[i], day(0), (n-i)*10) // строго убывающий опыт — детерминированный порядок
	}

	seen := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > n {
			t.Fatal("пагинация не остановилась — похоже на бесконечный цикл")
		}
		page, err := f.svc.List(ctx, ids[0], cursor, 3)
		if err != nil {
			t.Fatalf("List(cursor=%q): %v", cursor, err)
		}
		for _, e := range page.Items {
			if seen[e.UserID.String()] {
				t.Fatalf("пользователь %s встретился на двух страницах", e.UserID)
			}
			seen[e.UserID.String()] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	if len(seen) != n {
		t.Fatalf("уникальных пользователей за все страницы: %d, ожидалось %d", len(seen), n)
	}
}

// «me» приходит независимо от текущей страницы — контракт требует именно
// этого явно («иначе свою строку пришлось бы искать по всем страницам»).
func TestLeaderboardMeIsIndependentOfPage(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()

	me := uuid.New()
	f.seedPet(t, me, "Me", 0)
	f.seedXP(t, me, day(0), 1) // низкий опыт — окажется на последней позиции

	for i := range 5 {
		other := uuid.New()
		f.seedPet(t, other, uuid.New().String()[:8], 0)
		f.seedXP(t, other, day(0), 100+i) // все выше меня
	}

	// Первая страница из 2 строк не содержит меня (я шестой), но me обязан прийти.
	page, err := f.svc.List(ctx, me, "", 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Me == nil {
		t.Fatal("page.Me пустой, хотя у пользователя есть питомец")
	}
	if page.Me.Nickname != "Me" {
		t.Errorf("page.Me.Nickname = %q, ожидалось Me", page.Me.Nickname)
	}
	if !page.Me.IsMe {
		t.Error("page.Me.IsMe = false")
	}
	if page.Me.Rank != 6 {
		t.Errorf("page.Me.Rank = %d, ожидался 6 (последний из 6 участников)", page.Me.Rank)
	}
	for _, e := range page.Items {
		if e.IsMe {
			t.Error("IsMe=true у строки первой страницы — я туда не должен попасть при лимите 2")
		}
	}
}

// У запрашивающего без питомца нет строки в рейтинге — page.Me остаётся nil,
// а не ошибкой: у пользователя, который ещё не завёл питомца, всё ещё должна
// открываться страница лидерборда (контракт помечает Me необязательным).
func TestLeaderboardMeIsNilWithoutPet(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()

	other := uuid.New()
	f.seedPet(t, other, "Other", 0)
	f.seedXP(t, other, day(0), 5)

	page, err := f.svc.List(ctx, uuid.New(), "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Me != nil {
		t.Errorf("page.Me = %+v, ожидался nil — у запрашивающего нет питомца", page.Me)
	}
	if len(page.Items) != 1 {
		t.Fatalf("строк %d, ожидалась 1 (Other)", len(page.Items))
	}
}

// Уровень в строке лидерборда считается по той же кривой, что и внутри
// питомца, — не хардкожен и не расходится с internal/pet.
func TestLeaderboardLevelMatchesCurve(t *testing.T) {
	f := newSeedFixture(t)
	ctx := context.Background()

	userID := uuid.New()
	totalXP := 250 // 100 (ур.1→2) + 130 (ур.2→3) = 230, плюс 20 внутрь уровня 3
	f.seedPet(t, userID, "Leveled", totalXP)
	f.seedXP(t, userID, day(0), 1)

	want, err := config.LevelFor(totalXP, config.DefaultCurve)
	if err != nil {
		t.Fatalf("LevelFor: %v", err)
	}

	page, err := f.svc.List(ctx, userID, "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("строк %d, ожидалась 1", len(page.Items))
	}
	if page.Items[0].Level != want.Level {
		t.Errorf("Level = %d, LevelFor(%d) даёт %d — разошлись", page.Items[0].Level, totalXP, want.Level)
	}
}
