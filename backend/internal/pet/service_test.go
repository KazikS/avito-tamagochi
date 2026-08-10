package pet

import (
	"errors"
	"math"
	"testing"
	"time"
)

// t0 — произвольная фиксированная точка отсчёта. Домен не читает часы, поэтому
// конкретное значение ни на что не влияет; важно лишь, что оно одно на все
// тесты и не берётся из time.Now().
var t0 = time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)

// hours — интервал в часах как time.Duration, без потери точности на дробных
// значениях (1.5 часа — это 90 минут, а не 1 час).
func hours(h float64) time.Duration {
	return time.Duration(h * float64(time.Hour))
}

func full() Stats { return Stats{Hunger: 100, Joy: 100, Clean: 100, Energy: 100} }

// --- Распад -----------------------------------------------------------------

// invariant:5 — время в домене параметр; распад считается на интервал целиком.
//
// Ожидания посчитаны на бумаге по ставкам DefaultEconomy (голод 4, радость 3,
// чистота 2, энергия 3 в час), а не получены прогоном реализации.
func TestDecayMatchesHandComputedValues(t *testing.T) {
	cases := []struct {
		name  string
		start Stats
		after float64
		want  Stats
	}{
		{
			name:  "два часа — ровные числа",
			start: full(),
			after: 2,
			want:  Stats{Hunger: 92, Joy: 94, Clean: 96, Energy: 94},
		},
		{
			// 3/ч за 1.5 ч — это 4.5: округление до ближайшего (не вниз, см.
			// комментарий у roundToStat) даёт 96, а не 95: 100−4.5=95.5,
			// round-half-away-from-zero → 96.
			name:  "полтора часа — округление до ближайшего, не вниз",
			start: full(),
			after: 1.5,
			want:  Stats{Hunger: 94, Joy: 96, Clean: 97, Energy: 96},
		},
		{
			name:  "сутки без захода — питомец жив, но плох",
			start: full(),
			after: 24,
			want:  Stats{Hunger: 4, Joy: 28, Clean: 52, Energy: 28},
		},
		{
			name:  "двое суток — упор в ноль, ниже не уходим",
			start: full(),
			after: 48,
			want:  Stats{Hunger: 0, Joy: 0, Clean: 4, Energy: 0},
		},
		{
			name:  "нулевой интервал ничего не меняет",
			start: Stats{Hunger: 63, Joy: 12, Clean: 99, Energy: 7},
			after: 0,
			want:  Stats{Hunger: 63, Joy: 12, Clean: 99, Energy: 7},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Decay(c.start, false, t0, t0.Add(hours(c.after)), DefaultEconomy)
			if got != c.want {
				t.Fatalf("Decay(%+v, %v ч) = %+v, ожидалось %+v", c.start, c.after, got, c.want)
			}
		})
	}
}

// invariant:5 — распад на интервал совпадает с независимой замкнутой формой.
//
// Реализация округляет (start - rate*hours) вызовом math.Round. Модель здесь
// округляет ТО ЖЕ выражение через floor(y + 0.5), а не через math.Round —
// иной механизм округления той же величины, а не порядок действий, который
// её вычисляет. Раньше в этом тесте модель раскладывала выражение на
// start - round(rate*hours) и звала это «алгебраически тем же самым»; это
// оказалось неверно и найдено этим же тестом: round(n−x) ≠ n−round(x) на
// половинных значениях (50 − 7.5 = 42.5 → округляется в 43, а не в
// 50 − round(7.5) = 50 − 8 = 42) — округление до ближайшего чётного/нечётного
// не переносится через вычитание линейно. Ошибка в знаке, в единицах (час
// против минуты) или в направлении округления по-прежнему ломает совпадение.
func TestDecayAgreesWithIndependentClosedForm(t *testing.T) {
	intervals := []float64{0.1, 0.25, 0.5, 1, 1.5, 2, 3.75, 7, 12, 23.5}
	starts := []int{100, 87, 50, 13, 1}

	for _, iv := range intervals {
		for _, start := range starts {
			s := Stats{Hunger: start, Joy: start, Clean: start, Energy: start}
			got := Decay(s, false, t0, t0.Add(hours(iv)), DefaultEconomy)

			for _, k := range StatKeys {
				rate := DefaultEconomy.DecayPerHour[k]
				y := float64(start) - rate*iv
				want := int(math.Floor(y + 0.5))
				if want < 0 {
					want = 0
				}
				actual, err := got.Get(k)
				if err != nil {
					t.Fatalf("Get(%q): %v", k, err)
				}
				if actual != want {
					t.Errorf("показатель %q: старт %d, %v ч, ставка %v — получено %d, замкнутая форма даёт %d",
						k, start, iv, rate, actual, want)
				}
			}
		}
	}
}

// invariant:5 — распад монотонен и не выходит за границы контракта.
//
// Свойство, а не пример: чем дольше интервал, тем ниже показатель, и ни при
// каком интервале значение не покидает 0..100.
func TestDecayIsMonotoneAndStaysInRange(t *testing.T) {
	start := full()
	prev := start

	for step := 1; step <= 200; step++ {
		got := Decay(start, false, t0, t0.Add(hours(float64(step)/4)), DefaultEconomy)

		if !got.Valid() {
			t.Fatalf("шаг %d: %+v вышел за 0..100", step, got)
		}
		for _, k := range StatKeys {
			now, err := got.Get(k)
			if err != nil {
				t.Fatalf("Get(%q): %v", k, err)
			}
			before, err := prev.Get(k)
			if err != nil {
				t.Fatalf("Get(%q): %v", k, err)
			}
			if now > before {
				t.Fatalf("шаг %d, показатель %q: вырос с %d до %d — распад не может лечить",
					step, k, before, now)
			}
		}
		prev = got
	}
}

// Часы, идущие назад, не должны лечить питомца: иначе сдвиг часов демо-стенда
// назад становится способом накрутки.
func TestDecayIgnoresBackwardsClock(t *testing.T) {
	start := Stats{Hunger: 40, Joy: 40, Clean: 40, Energy: 40}
	got := Decay(start, false, t0, t0.Add(-hours(10)), DefaultEconomy)
	if got != start {
		t.Fatalf("Decay назад во времени = %+v, ожидалось без изменений %+v", got, start)
	}
}

func TestDecayRestoresEnergyWhileSleeping(t *testing.T) {
	start := Stats{Hunger: 100, Joy: 100, Clean: 100, Energy: 10}

	// 4 часа сна: энергия 10 + 12*4 = 58, остальные показатели падают как обычно.
	got := Decay(start, true, t0, t0.Add(hours(4)), DefaultEconomy)
	want := Stats{Hunger: 84, Joy: 88, Clean: 92, Energy: 58}
	if got != want {
		t.Fatalf("сон 4 ч: получено %+v, ожидалось %+v", got, want)
	}

	// Долгий сон упирается в потолок, а не пробивает его.
	long := Decay(start, true, t0, t0.Add(hours(50)), DefaultEconomy)
	if long.Energy != 100 {
		t.Fatalf("энергия после 50 ч сна = %d, ожидался потолок 100", long.Energy)
	}
}

// FuzzDecayStaysInRange ищет вход, на котором распад выдаёт значение вне
// 0..100. Такое значение не проходит валидацию по собственной спеке проекта,
// то есть сервер отдал бы фронту ответ, который сам же считает некорректным.
func FuzzDecayStaysInRange(f *testing.F) {
	f.Add(100, 50, 30, 0, int64(time.Hour), false)
	f.Add(0, 0, 0, 0, int64(-time.Hour), true)
	f.Add(100, 100, 100, 100, int64(math.MaxInt64), false)

	f.Fuzz(func(t *testing.T, h, j, c, e int, d int64, sleeping bool) {
		s := Stats{Hunger: clampStat(h), Joy: clampStat(j), Clean: clampStat(c), Energy: clampStat(e)}
		got := Decay(s, sleeping, t0, t0.Add(time.Duration(d)), DefaultEconomy)
		if !got.Valid() {
			t.Fatalf("Decay(%+v, sleeping=%v, %v) = %+v — вне 0..100", s, sleeping, time.Duration(d), got)
		}
	})
}

// --- Настроение -------------------------------------------------------------

// Настроение — производная от МИНИМАЛЬНОГО показателя (так сказано в контракте
// у PetMood). Границы проверяются с обеих сторон: порог и порог минус один.
func TestMoodIsDerivedFromMinimumStat(t *testing.T) {
	cases := []struct {
		min        int
		wantMood   Mood
		wantMultip float64
	}{
		{100, MoodRadiant, 1.25},
		{80, MoodRadiant, 1.25},
		{79, MoodHappy, 1.10},
		{60, MoodHappy, 1.10},
		{59, MoodNeutral, 1.00},
		{40, MoodNeutral, 1.00},
		{39, MoodSad, 0.90},
		{20, MoodSad, 0.90},
		{19, MoodSick, 0.75},
		{0, MoodSick, 0.75},
	}

	for _, c := range cases {
		// Минимум ставим в energy, остальные держим полными — так тест
		// заодно проверяет, что берётся минимум, а не первый попавшийся.
		s := Stats{Hunger: 100, Joy: 100, Clean: 100, Energy: c.min}
		mood, mul := MoodOf(s, false, DefaultEconomy)
		if mood != c.wantMood || mul != c.wantMultip {
			t.Errorf("минимум %d: получено (%q, %v), ожидалось (%q, %v)",
				c.min, mood, mul, c.wantMood, c.wantMultip)
		}
	}
}

func TestMoodOfSleepingPetIsSleeping(t *testing.T) {
	mood, mul := MoodOf(Stats{}, true, DefaultEconomy)
	if mood != MoodSleeping || mul != 1 {
		t.Fatalf("спящий питомец: (%q, %v), ожидалось (%q, 1)", mood, mul, MoodSleeping)
	}
}

// --- Действия ухода ---------------------------------------------------------

// Контракт формулирует потолок примером: «82 + 30 = 100, остаток сгорает».
// Здесь ровно этот пример.
func TestActionCeilingBurnsTheRemainder(t *testing.T) {
	s := Stats{Hunger: 82, Joy: 50, Clean: 50, Energy: 50}
	out, err := ApplyAction(ActionFeed, s, false, DefaultEconomy)
	if err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if out.Stats.Hunger != 100 {
		t.Fatalf("82 + 30 дало %d, контракт требует 100", out.Stats.Hunger)
	}
	if out.StatCapped {
		t.Error("StatCapped=true, хотя показатель до действия был 82, а не 100")
	}
	if out.BaseXP != 10 {
		t.Errorf("базовый опыт %d, ожидалось 10", out.BaseXP)
	}
}

// Контракт: «statCapped=true означает: показатель уже был 100 ДО действия,
// XP не начислен. Фронт всё равно проигрывает анимацию». То есть это исход,
// а не ошибка.
func TestFeedingAFullPetIsAllowedAndGivesNoXP(t *testing.T) {
	s := full()
	out, err := ApplyAction(ActionFeed, s, false, DefaultEconomy)
	if err != nil {
		t.Fatalf("кормление сытого вернуло ошибку %v, ожидался исход без опыта", err)
	}
	if !out.StatCapped {
		t.Error("StatCapped=false, хотя показатель был 100 до действия")
	}
	if out.BaseXP != 0 {
		t.Errorf("базовый опыт %d, ожидался 0", out.BaseXP)
	}
	if out.Stats != s {
		t.Errorf("показатели изменились: %+v, ожидалось %+v", out.Stats, s)
	}
}

func TestSleepAndWakeGuards(t *testing.T) {
	awake := full()

	out, err := ApplyAction(ActionSleep, awake, false, DefaultEconomy)
	if err != nil {
		t.Fatalf("уложить бодрствующего: %v", err)
	}
	if !out.Sleeping {
		t.Error("после sleep питомец не спит")
	}

	if _, sleepErr := ApplyAction(ActionSleep, awake, true, DefaultEconomy); !errors.Is(sleepErr, ErrAlreadyAsleep) {
		t.Errorf("сон спящего: %v, ожидалось ErrAlreadyAsleep", sleepErr)
	}
	if _, wakeErr := ApplyAction(ActionWake, awake, false, DefaultEconomy); !errors.Is(wakeErr, ErrNotAsleep) {
		t.Errorf("разбудить бодрствующего: %v, ожидалось ErrNotAsleep", wakeErr)
	}
	if _, careErr := ApplyAction(ActionFeed, awake, true, DefaultEconomy); !errors.Is(careErr, ErrAsleep) {
		t.Errorf("кормление спящего: %v, ожидалось ErrAsleep", careErr)
	}
}

func TestUnknownActionIsRejected(t *testing.T) {
	if _, err := ApplyAction(ActionKind("dance"), full(), false, DefaultEconomy); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("неизвестное действие: %v, ожидалось ErrUnknownAction", err)
	}
}

// --- Опыт -------------------------------------------------------------------

// Контракт задаёт ОДНО произведение и ОДНО округление вниз:
// «XP = базовый × множитель_настроения × множитель_стрика … Округление вниз».
//
// Вход подобран так, что порядок округления виден в результате:
// 5 × 1.1 × 1.9 = 10.45 → 10 при одном округлении в конце,
// но floor(5 × 1.1) = 5, 5 × 1.9 = 9.5 → 9 при округлении после каждого шага.
// Тест падает, если кто-то округлит дважды.
func TestXPRoundsDownOnceAfterBothMultipliers(t *testing.T) {
	got, err := AwardXP(5, 1.1, 1.9, 0, 40)
	if err != nil {
		t.Fatalf("AwardXP: %v", err)
	}
	if got.Granted != 10 {
		t.Fatalf("начислено %d, ожидалось 10 (округление одно, после обоих множителей; 9 означает два округления)",
			got.Granted)
	}
}

// Контракт, правило 5: «Суточный потолок ухода обрезает последнее действие
// ЧАСТИЧНО (осталось 5 из 40 — начисляем 5, не 0)». Это дословно тот пример.
func TestDailyCapClipsTheLastActionPartially(t *testing.T) {
	got, err := AwardXP(10, 1, 1, 35, 40)
	if err != nil {
		t.Fatalf("AwardXP: %v", err)
	}
	if got.Granted != 5 {
		t.Fatalf("начислено %d, контракт требует 5 — остаток капа, а не ноль", got.Granted)
	}
	if !got.CapReached {
		t.Error("CapReached=false, хотя кап исчерпан ровно этим начислением")
	}
}

func TestXPAwardEdges(t *testing.T) {
	cases := []struct {
		name           string
		base           int
		mood, streak   float64
		spent, cap     int
		wantGranted    int
		wantCapReached bool
	}{
		{"кап уже исчерпан — ноль", 10, 1, 1, 40, 40, 0, true},
		{"кап перебран — ноль, а не отрицательное", 10, 1, 1, 50, 40, 0, true},
		{"обычное начисление", 10, 1.25, 1, 0, 40, 12, false},
		{"нулевой базовый опыт", 0, 1.25, 1.2, 0, 40, 0, false},
		{"нулевой кап запрещает начисление", 10, 1, 1, 0, 0, 0, true},
		{"одно действие не может превысить кап", 1000, 1, 1, 0, 40, 40, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := AwardXP(c.base, c.mood, c.streak, c.spent, c.cap)
			if err != nil {
				t.Fatalf("AwardXP: %v", err)
			}
			if got.Granted != c.wantGranted {
				t.Errorf("начислено %d, ожидалось %d", got.Granted, c.wantGranted)
			}
			if got.CapReached != c.wantCapReached {
				t.Errorf("CapReached=%v, ожидалось %v", got.CapReached, c.wantCapReached)
			}
		})
	}
}

func TestXPRejectsNonsenseInput(t *testing.T) {
	cases := []struct {
		name            string
		base            int
		mood, streak    float64
		spent, dailyCap int
	}{
		{"отрицательный базовый опыт", -1, 1, 1, 0, 40},
		{"отрицательный кап", 10, 1, 1, 0, -1},
		{"отрицательный расход", 10, 1, 1, -1, 40},
		{"NaN в множителе настроения", 10, math.NaN(), 1, 0, 40},
		{"бесконечность в множителе стрика", 10, 1, math.Inf(1), 0, 40},
		{"отрицательный множитель", 10, -1, 1, 0, 40},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := AwardXP(c.base, c.mood, c.streak, c.spent, c.dailyCap); !errors.Is(err, ErrInvalidEconomy) {
				t.Fatalf("получено %v, ожидалась ErrInvalidEconomy", err)
			}
		})
	}
}

// FuzzAwardXPNeverExceedsCap ищет вход, на котором суточный кап пробивается.
// Кап — единственное, что защищает от накрутки опыта (docs/ARCHITECTURE.md,
// инвариант 1), поэтому его нарушение — это не косметика.
func FuzzAwardXPNeverExceedsCap(f *testing.F) {
	f.Add(10, 1.0, 1.0, 0, 40)
	f.Add(1000, 5.0, 5.0, 39, 40)

	f.Fuzz(func(t *testing.T, base int, mood, streak float64, spent, dailyCap int) {
		got, err := AwardXP(base, mood, streak, spent, dailyCap)
		if err != nil {
			return // отвергнутый вход — корректное поведение, а не находка
		}
		if got.Granted < 0 {
			t.Fatalf("начислено отрицательное: %d", got.Granted)
		}
		if spent+got.Granted > dailyCap {
			t.Fatalf("кап пробит: потрачено %d + начислено %d > кап %d", spent, got.Granted, dailyCap)
		}
	})
}

// --- Экономика --------------------------------------------------------------

func TestDefaultEconomyIsValid(t *testing.T) {
	if err := DefaultEconomy.Validate(); err != nil {
		t.Fatalf("экономика, с которой собирается сервер, невалидна: %v", err)
	}
}

// Validate обязана ловить именно те поломки, ради которых заведена. Проверяем
// каждую отдельно: «валидатор вернул ошибку» без указания, на что именно, —
// это гейт, который мог сработать по другой причине.
func TestEconomyValidateRejectsBrokenTables(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(e *Economy)
	}{
		{"нет ставки распада", func(e *Economy) { delete(e.DecayPerHour, StatJoy) }},
		{"отрицательная ставка распада", func(e *Economy) { e.DecayPerHour[StatJoy] = -1 }},
		{"NaN в ставке распада", func(e *Economy) { e.DecayPerHour[StatJoy] = math.NaN() }},
		{"нет действия", func(e *Economy) { delete(e.Actions, ActionWash) }},
		{"отрицательный опыт действия", func(e *Economy) {
			a := e.Actions[ActionFeed]
			a.XP = -5
			e.Actions[ActionFeed] = a
		}},
		{"действие с нулевым эффектом", func(e *Economy) {
			a := e.Actions[ActionFeed]
			a.Effect = 0
			e.Actions[ActionFeed] = a
		}},
		{"действие ссылается на несуществующий показатель", func(e *Economy) {
			a := e.Actions[ActionFeed]
			a.Stat = "vibes"
			e.Actions[ActionFeed] = a
		}},
		{"пороги настроения пусты", func(e *Economy) { e.MoodTiers = nil }},
		{"пороги настроения не убывают", func(e *Economy) {
			e.MoodTiers = []MoodTier{{Mood: MoodSad, MinStat: 20, Multiplier: 1}, {Mood: MoodSick, MinStat: 50, Multiplier: 1}}
		}},
		{"последний порог не ноль", func(e *Economy) {
			e.MoodTiers = []MoodTier{{Mood: MoodSick, MinStat: 10, Multiplier: 1}}
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := cloneEconomy(DefaultEconomy)
			c.corrupt(&e)
			if err := e.Validate(); !errors.Is(err, ErrInvalidEconomy) {
				t.Fatalf("получено %v, ожидалась ErrInvalidEconomy", err)
			}
		})
	}
}

// cloneEconomy делает копию с собственными map: без этого поломка в одном
// подтесте протекала бы в остальные через общий DefaultEconomy.
func cloneEconomy(e Economy) Economy {
	out := e
	out.DecayPerHour = make(map[StatKey]float64, len(e.DecayPerHour))
	for k, v := range e.DecayPerHour {
		out.DecayPerHour[k] = v
	}
	out.Actions = make(map[ActionKind]Action, len(e.Actions))
	for k, v := range e.Actions {
		out.Actions[k] = v
	}
	out.MoodTiers = append([]MoodTier(nil), e.MoodTiers...)
	return out
}

// Каждое настроение из перечисления контракта, кроме sleeping, обязано быть
// достижимым: недостижимое настроение — мёртвая ветка в клиенте.
func TestEveryMoodTierIsReachable(t *testing.T) {
	seen := make(map[Mood]bool)
	for stat := statMin; stat <= statMax; stat++ {
		mood, _ := MoodOf(Stats{Hunger: 100, Joy: 100, Clean: 100, Energy: stat}, false, DefaultEconomy)
		seen[mood] = true
	}
	for _, tier := range DefaultEconomy.MoodTiers {
		if !seen[tier.Mood] {
			t.Errorf("настроение %q не достигается ни при каком показателе", tier.Mood)
		}
	}
}
