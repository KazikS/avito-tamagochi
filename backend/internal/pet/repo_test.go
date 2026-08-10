package pet_test

// Интеграционные тесты слоя данных. Живут в пакете pet_test (внешнем),
// а не в pet: так они видят ровно ту поверхность, которой пользуется
// остальной код, и не могут случайно опереться на неэкспортированную деталь.
//
// Без TEST_DATABASE_URL пропускаются; в CI переменная выставлена, поэтому там
// они выполняются по-настоящему (см. .github/workflows/ci.yml).

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
	"tamagochi/internal/pet"
	"tamagochi/pkg/clock"
	"tamagochi/pkg/pgtest"
)

// base — момент, от которого отсчитывают все тесты. Фиксированный: часы в
// сервис приходят зависимостью (pkg/clock), а не из time.Now().
var base = time.Date(2026, time.August, 9, 9, 0, 0, 0, time.UTC)

// fixture — сервис, поднятый на личной схеме теста, и его управляемые часы.
type fixture struct {
	svc   *pet.Service
	clk   *clock.Fixed
	pool  *pgxpool.Pool
	actor pet.Actor
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	pool := pgtest.Pool(t)
	clk := clock.NewFixed(base)

	svc, err := pet.NewService(pet.NewRepo(pool), clk, pet.DefaultEconomy, config.DefaultCurve, config.DefaultDailyCareXPCap)
	if err != nil {
		t.Fatalf("сборка сервиса: %v", err)
	}

	return &fixture{
		svc:   svc,
		clk:   clk,
		pool:  pool,
		actor: pet.Actor{UserID: uuid.New(), Location: time.UTC},
	}
}

// withPet заводит питомца и возвращает готовую обвязку.
func withPet(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	if _, err := f.svc.Create(context.Background(), f.actor, "green", "Ави"); err != nil {
		t.Fatalf("создание питомца: %v", err)
	}
	return f
}

// logRows считает записи журнала действий пользователя.
func (f *fixture) logRows(t *testing.T) int {
	t.Helper()
	var n int
	err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pet_action_log WHERE user_id = $1`, f.actor.UserID).Scan(&n)
	if err != nil {
		t.Fatalf("подсчёт журнала: %v", err)
	}
	return n
}

// logRowsForAction считает записи журнала с конкретным actionID.
//
// Отдельно от logRows: несколько раундов гонки на одном питомце используют
// общий журнал, и «ровно одна запись у ЭТОГО actionID» — не то же самое, что
// «в журнале всего одна запись».
func (f *fixture) logRowsForAction(t *testing.T, actionID uuid.UUID) int {
	t.Helper()
	var n int
	err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pet_action_log WHERE user_id = $1 AND action_id = $2`,
		f.actor.UserID, actionID).Scan(&n)
	if err != nil {
		t.Fatalf("подсчёт журнала по actionID: %v", err)
	}
	return n
}

// storedStatsAt читает момент снимка показателей прямо из базы.
func (f *fixture) storedStatsAt(t *testing.T) time.Time {
	t.Helper()
	var at time.Time
	err := f.pool.QueryRow(context.Background(),
		`SELECT stats_at FROM pets WHERE user_id = $1`, f.actor.UserID).Scan(&at)
	if err != nil {
		t.Fatalf("чтение stats_at: %v", err)
	}
	return at
}

// --- Инвариант 1: опыт только вместе с записью в журнал ---------------------

// invariant:1 — повтор действия с тем же actionId не начисляет опыт второй раз.
//
// Контракт: «UUID с клиента. Защищает от двойного тапа: повтор возвращает
// прежний результат».
func TestRepeatedActionIsIdempotent(t *testing.T) {
	f := withPet(t)
	ctx := context.Background()
	actionID := uuid.New()

	// Уводим сытость вниз, иначе кормление упрётся в потолок и опыт будет 0
	// по совсем другой причине — тест перестал бы проверять идемпотентность.
	f.clk.Advance(10 * time.Hour)

	first, _, err := f.svc.Act(ctx, f.actor, actionID, pet.ActionFeed)
	if err != nil {
		t.Fatalf("первое действие: %v", err)
	}
	if first.XPGained == 0 {
		t.Fatalf("первое действие не начислило опыт — тест ничего не проверит")
	}

	// Часы идут вперёд: если повтор пересчитает состояние, а не вернёт
	// сохранённое, показатели в ответе разойдутся с первым ответом.
	f.clk.Advance(3 * time.Hour)

	second, secondReplayed, err := f.svc.Act(ctx, f.actor, actionID, pet.ActionFeed)
	if err != nil {
		t.Fatalf("повтор действия: %v", err)
	}
	if !secondReplayed {
		t.Error("Act не сообщил replayed=true на повторе — вызывающий (WS-пуш) не узнает, что реального изменения не было")
	}

	if second.XPGained != first.XPGained {
		t.Errorf("повтор начислил %d опыта, первый раз — %d", second.XPGained, first.XPGained)
	}
	if second.Pet.TotalXP != first.Pet.TotalXP {
		t.Errorf("повтор вернул totalXp=%d, первый раз — %d", second.Pet.TotalXP, first.Pet.TotalXP)
	}
	if second.Pet.Stats != first.Pet.Stats {
		t.Errorf("повтор вернул показатели %+v, первый раз — %+v", second.Pet.Stats, first.Pet.Stats)
	}
	if n := f.logRows(t); n != 1 {
		t.Errorf("записей в журнале %d, ожидалась одна", n)
	}

	// И главное: суммарный опыт в базе вырос ровно на одно начисление.
	got, err := f.svc.Get(ctx, f.actor)
	if err != nil {
		t.Fatalf("чтение питомца: %v", err)
	}
	if got.TotalXP != first.XPGained {
		t.Errorf("суммарный опыт %d, ожидался %d — опыт начислен дважды", got.TotalXP, first.XPGained)
	}
}

// invariant:1 — суточный кап обрезает начисление частично на настоящем пути,
// а не только в чистой функции.
//
// Кормим, пока кап не исчерпается, и проверяем, что сумма начисленного за
// сутки равна ровно капу — ни больше, ни меньше.
func TestDailyCapIsEnforcedAcrossActions(t *testing.T) {
	f := withPet(t)
	ctx := context.Background()

	// Роняем показатели один раз в начале, а не между действиями. Отматывать
	// время внутри цикла нельзя: суммарный сдвиг перешагнул бы полночь, кап
	// обнулился бы по смене суток, и тест проверял бы не то, что заявляет.
	f.clk.Advance(30 * time.Hour)
	dayAtStart := f.clk.Now().UTC().Day()

	total := 0
	capReportedAtLeastOnce := false
	kinds := []pet.ActionKind{pet.ActionFeed, pet.ActionPlay, pet.ActionWash}
	for round := range 6 {
		for _, kind := range kinds {
			res, _, err := f.svc.Act(ctx, f.actor, uuid.New(), kind)
			if errors.Is(err, pet.ErrDailyLimit) {
				continue
			}
			if err != nil {
				t.Fatalf("круг %d, действие %q: %v", round, kind, err)
			}
			total += res.XPGained
			if res.CareXPCapReached {
				capReportedAtLeastOnce = true
			}
		}
	}

	if f.clk.Now().UTC().Day() != dayAtStart {
		t.Fatalf("тест перешагнул полночь — суточный кап обнулился, проверка недействительна")
	}
	if total > config.DefaultDailyCareXPCap {
		t.Fatalf("за сутки начислено %d опыта при капе %d", total, config.DefaultDailyCareXPCap)
	}
	// Без этого тест был бы зелёным и при капе, который никогда не срабатывает:
	// «не превысили» — свойство, которое держится само собой, если начислений мало.
	if !capReportedAtLeastOnce {
		t.Fatalf("кап ни разу не сработал (начислено %d из %d) — тест ничего не проверил",
			total, config.DefaultDailyCareXPCap)
	}

	got, err := f.svc.Get(ctx, f.actor)
	if err != nil {
		t.Fatalf("чтение питомца: %v", err)
	}
	if got.TotalXP != total {
		t.Errorf("суммарный опыт в базе %d, начислено по ответам %d", got.TotalXP, total)
	}
}

// --- Инвариант 4: однократность при гонке -----------------------------------

// invariant:4 — параллельный двойной тап начисляет опыт ровно один раз.
//
// Это то, ради чего в схеме PRIMARY KEY (user_id, action_id) и SELECT ...
// FOR UPDATE. Проверкой «а нет ли уже такой записи» в коде инвариант не
// держится: между проверкой и вставкой помещается второй запрос.
//
// raceRound — одна попытка столкновения: racers горутин с ОДНИМ actionID,
// отпущенных одновременно через закрытие канала.
//
// Собственный измеренный факт про эту гонку, а не предположение: на восьми
// горутинах реальное столкновение на уровне базы происходит не каждый раз —
// на мутации без FOR UPDATE (SELECT без блокировки строки) прогон ловил
// поломку в 9 из 20 запусков (`go test -count=20`). Слишком ненадёжно для
// гейта. Шестнадцать горутин на раунд и пять независимых раундов подряд
// (TestConcurrentSameActionGrantsExactlyOnce) ловят ту же мутацию с первой
// попытки — дальше не подбирал точнее, этого достаточно для гейта.
// raceRound возвращает начисленный опыт этого раунда, чтобы вызывающий тест
// мог проверить накопленный итог: питомец общий на все раунды, поэтому
// TotalXP растёт, а не сбрасывается между ними.
func raceRound(t *testing.T, f *fixture, racers int) (xpGained int) {
	t.Helper()
	ctx := context.Background()
	actionID := uuid.New()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []pet.ActResult
		errs    []error
	)

	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // стартуем одновременно, иначе гонки может не случиться
			res, _, err := f.svc.Act(ctx, f.actor, actionID, pet.ActionFeed)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			results = append(results, res)
		}()
	}
	close(start)
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("часть запросов упала: %v", errs)
	}
	if len(results) != racers {
		t.Fatalf("успешных ответов %d, ожидалось %d", len(results), racers)
	}

	// Все ответы обязаны быть одинаковыми: победила одна вставка, остальные
	// прочитали её результат.
	for i, r := range results[1:] {
		if r.XPGained != results[0].XPGained || r.Pet.Stats != results[0].Pet.Stats {
			t.Errorf("ответ %d разошёлся с первым: %+v против %+v", i+1, r, results[0])
		}
	}

	if n := f.logRowsForAction(t, actionID); n != 1 {
		t.Fatalf("записей в журнале с этим actionID %d, ожидалась ровно одна", n)
	}

	return results[0].XPGained
}

// Тест обязан гоняться с -race, иначе он проверяет только последовательный
// путь; в CI -race включён для всего пакета.
func TestConcurrentSameActionGrantsExactlyOnce(t *testing.T) {
	f := withPet(t)
	f.clk.Advance(10 * time.Hour)

	// Каждый раунд бьёт по одному и тому же питомцу, но своим actionID —
	// новая попытка столкновения, независимая от предыдущей. Кормление не
	// упирается в суточный лимит (6 в сутки) на 5 раундах.
	const rounds = 5
	wantTotal := 0
	for round := range rounds {
		t.Run(fmt.Sprintf("раунд_%d", round+1), func(t *testing.T) {
			wantTotal += raceRound(t, f, 16)
		})
	}

	// Общий итог по питомцу — сумма начислений всех раундов, не больше и не
	// меньше. Проверка на накопленном состоянии, а не только по каждому
	// раунду в отдельности: она ловит и потерянное обновление total_xp между
	// раундами, которое проверка внутри одного раунда не увидит.
	got, err := f.svc.Get(context.Background(), f.actor)
	if err != nil {
		t.Fatalf("чтение питомца: %v", err)
	}
	if got.TotalXP != wantTotal {
		t.Fatalf("суммарный опыт по всем раундам %d, ожидалось %d", got.TotalXP, wantTotal)
	}
}

// Разные действия, поданные одновременно, не должны терять начисления:
// сериализация по строке питомца обязана их выстроить, а не склеить.
func TestConcurrentDistinctActionsAllApply(t *testing.T) {
	f := withPet(t)
	ctx := context.Background()

	f.clk.Advance(12 * time.Hour)

	const racers = 5
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, _ = f.svc.Act(ctx, f.actor, uuid.New(), pet.ActionFeed)
		}()
	}
	close(start)
	wg.Wait()

	// Лимит кормления — 6 в сутки, поэтому все пять обязаны пройти.
	if n := f.logRows(t); n != racers {
		t.Fatalf("записей в журнале %d, ожидалось %d — часть действий потерялась", n, racers)
	}
}

// --- Чтение не меняет состояние ---------------------------------------------

// Обещание из преамбулы пакета internal/pet: показатели пересчитываются при
// чтении и НЕ записываются обратно. Иначе питомец «худеет» тем быстрее, чем
// чаще открывают экран, — наблюдение меняло бы наблюдаемое.
//
// Тест смотрит в базу напрямую: снимок и его момент обязаны остаться теми же
// после любого числа чтений, при том что возвращаемые показатели падают.
func TestReadingDoesNotAgePet(t *testing.T) {
	f := withPet(t)
	ctx := context.Background()

	atCreate := f.storedStatsAt(t)

	first, err := f.svc.Get(ctx, f.actor)
	if err != nil {
		t.Fatalf("первое чтение: %v", err)
	}

	// Много чтений вперемешку с ходом часов.
	for range 20 {
		f.clk.Advance(30 * time.Minute)
		if _, readErr := f.svc.Get(ctx, f.actor); readErr != nil {
			t.Fatalf("чтение: %v", readErr)
		}
	}

	if got := f.storedStatsAt(t); !got.Equal(atCreate) {
		t.Errorf("stats_at в базе сдвинулся с %v на %v — чтение записало состояние", atCreate, got)
	}

	// Контрольная проверка, что тест вообще что-то ловит: показатели за
	// 10 часов обязаны просесть, иначе часы не двигались и сравнение выше
	// было бы бессмысленным.
	last, err := f.svc.Get(ctx, f.actor)
	if err != nil {
		t.Fatalf("последнее чтение: %v", err)
	}
	if last.Stats.Hunger >= first.Stats.Hunger {
		t.Fatalf("сытость не изменилась (%d → %d) — часы не шли, тест ничего не проверил",
			first.Stats.Hunger, last.Stats.Hunger)
	}
}

// --- Создание питомца -------------------------------------------------------

func TestSecondPetForSameUserIsRejected(t *testing.T) {
	f := withPet(t)

	_, err := f.svc.Create(context.Background(), f.actor, "blue", "Второй")
	if !errors.Is(err, pet.ErrPetExists) {
		t.Fatalf("создание второго питомца вернуло %v, ожидалась ErrPetExists", err)
	}
}

func TestActionsWithoutPetReportNoPet(t *testing.T) {
	f := newFixture(t)

	if _, err := f.svc.Get(context.Background(), f.actor); !errors.Is(err, pet.ErrNoPet) {
		t.Errorf("чтение без питомца: %v, ожидалась ErrNoPet", err)
	}
	if _, _, err := f.svc.Act(context.Background(), f.actor, uuid.New(), pet.ActionFeed); !errors.Is(err, pet.ErrNoPet) {
		t.Errorf("действие без питомца: %v, ожидалась ErrNoPet", err)
	}
}

// Распад обязан досчитываться ДО действия: иначе одно нажатие «отменяет» часы
// простоя, и питомец, к которому не заходили сутки, становится сытым.
func TestDecayIsAppliedBeforeTheAction(t *testing.T) {
	f := withPet(t)
	ctx := context.Background()

	// Сутки без захода: сытость 100 - 4*24 = 4.
	f.clk.Advance(24 * time.Hour)

	res, _, err := f.svc.Act(ctx, f.actor, uuid.New(), pet.ActionFeed)
	if err != nil {
		t.Fatalf("кормление: %v", err)
	}

	// 4 + 30 = 34, а не 100 и не 130.
	if res.Pet.Stats.Hunger != 34 {
		t.Fatalf("сытость после суток простоя и кормления = %d, ожидалось 34 (4 + 30)", res.Pet.Stats.Hunger)
	}
}

// Суточный лимит действия — не то же самое, что кап опыта: он запрещает само
// действие, а не обнуляет начисление.
func TestActionDailyLimitIsEnforced(t *testing.T) {
	f := withPet(t)
	ctx := context.Background()

	dayAtStart := f.clk.Now().UTC().Day()

	// Время НЕ двигаем: тест проверяет лимит числа действий, а не распад, и
	// продвижение часов здесь — тот же капкан, что уже пойман в
	// TestDailyCapIsEnforcedAcrossActions: пять шагов по 6 часов от 09:00
	// переезжают за полночь и обнуляют именно тот счётчик, который проверяется.
	limit := pet.DefaultEconomy.Actions[pet.ActionWash].DailyLimit
	for i := range limit {
		if _, _, err := f.svc.Act(ctx, f.actor, uuid.New(), pet.ActionWash); err != nil {
			t.Fatalf("мытьё %d из %d: %v", i+1, limit, err)
		}
	}
	if f.clk.Now().UTC().Day() != dayAtStart {
		t.Fatalf("тест перешагнул полночь — суточный лимит обнулился, проверка недействительна")
	}

	_, _, err := f.svc.Act(ctx, f.actor, uuid.New(), pet.ActionWash)
	if !errors.Is(err, pet.ErrDailyLimit) {
		t.Fatalf("мытьё сверх лимита %d вернуло %v, ожидалась ErrDailyLimit", limit, err)
	}
}
