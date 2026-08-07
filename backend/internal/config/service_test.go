package config_test

import (
	"errors"
	"math"
	"testing"

	"tamagochi/internal/config"
)

// Значения посчитаны вручную по формуле base * factor^i с округлением до
// roundTo, а не сняты с реализации: тест, списанный с вывода кода, зелёный при
// любой её ошибке.
func TestCostsMatchHandComputedCurve(t *testing.T) {
	costs, err := config.Costs(config.DefaultCurve)
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	if got, want := len(costs), config.DefaultCurve.MaxLevel-1; got != want {
		t.Fatalf("переходов %d, ожидали %d", got, want)
	}

	want := []int{100, 130, 160, 200, 240, 310, 380, 480}
	for i, w := range want {
		if costs[i] != w {
			t.Errorf("переход на уровень %d: получили %d, ожидали %d", i+2, costs[i], w)
		}
	}
}

// Кривая с factor >= 1 не может дешеветь: иначе прогрессия ломается смыслово,
// а не арифметически, и ни один линтер этого не увидит.
func TestCostsAreNonDecreasing(t *testing.T) {
	costs, err := config.Costs(config.DefaultCurve)
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	for i := 1; i < len(costs); i++ {
		if costs[i] < costs[i-1] {
			t.Errorf("уровень %d дешевле предыдущего: %d < %d", i+2, costs[i], costs[i-1])
		}
	}
}

func TestCostsRejectsBrokenCurves(t *testing.T) {
	cases := map[string]config.Curve{
		"нулевая база":          {Base: 0, Factor: 1.25, RoundTo: 10, MaxLevel: 25},
		"отрицательная база":    {Base: -100, Factor: 1.25, RoundTo: 10, MaxLevel: 25},
		"нулевой множитель":     {Base: 100, Factor: 0, RoundTo: 10, MaxLevel: 25},
		"отрицательный множ.":   {Base: 100, Factor: -1.25, RoundTo: 10, MaxLevel: 25},
		"NaN-множитель":         {Base: 100, Factor: math.NaN(), RoundTo: 10, MaxLevel: 25},
		"бесконечный множ.":     {Base: 100, Factor: math.Inf(1), RoundTo: 10, MaxLevel: 25},
		"нулевое округление":    {Base: 100, Factor: 1.25, RoundTo: 0, MaxLevel: 25},
		"нулевой макс. уровень": {Base: 100, Factor: 1.25, RoundTo: 10, MaxLevel: 0},
	}

	for name, curve := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Costs(curve); !errors.Is(err, config.ErrInvalidCurve) {
				t.Errorf("получили %v, ожидали ErrInvalidCurve", err)
			}
		})
	}
}

// Один уровень — корректная кривая без единого перехода.
func TestCostsSingleLevelCurve(t *testing.T) {
	costs, err := config.Costs(config.Curve{Base: 100, Factor: 1.25, RoundTo: 10, MaxLevel: 1})
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if len(costs) != 0 {
		t.Errorf("переходов %d, ожидали 0", len(costs))
	}
}

// Крутая кривая не должна переполнять int: math.Pow даёт +Inf, а приведение
// +Inf к int в Go не определено — без потолка получались бы произвольные,
// в том числе отрицательные, стоимости.
func TestCostsClampInsteadOfOverflowing(t *testing.T) {
	costs, err := config.Costs(config.Curve{Base: 1000, Factor: 10, RoundTo: 1, MaxLevel: 200})
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	for i, cost := range costs {
		if cost <= 0 {
			t.Fatalf("стоимость перехода на уровень %d равна %d", i+2, cost)
		}
	}
}

// Округление вниз способно дать ноль (Base=1, RoundTo=10): бесплатный уровень
// сломал бы прогресс, поэтому минимум — единица.
func TestCostsNeverFree(t *testing.T) {
	costs, err := config.Costs(config.Curve{Base: 1, Factor: 1, RoundTo: 10, MaxLevel: 5})
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	for i, cost := range costs {
		if cost < 1 {
			t.Errorf("переход на уровень %d стоит %d", i+2, cost)
		}
	}
}

func TestLevelForBoundaries(t *testing.T) {
	curve := config.DefaultCurve
	costs, err := config.Costs(curve)
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	cases := []struct {
		name  string
		xp    int
		level int
		into  int
		next  int
	}{
		{"ноль опыта", 0, 1, 0, costs[0]},
		{"отрицательный опыт", -50, 1, 0, costs[0]},
		{"на единицу меньше порога", costs[0] - 1, 1, costs[0] - 1, 1},
		{"ровно порог", costs[0], 2, 0, costs[1]},
		{"порог плюс один", costs[0] + 1, 2, 1, costs[1] - 1},
		{"два порога", costs[0] + costs[1], 3, 0, costs[2]},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, levelErr := config.LevelFor(c.xp, curve)
			if levelErr != nil {
				t.Fatalf("LevelFor: %v", levelErr)
			}
			if got.Level != c.level || got.IntoLevel != c.into || got.ToNext != c.next {
				t.Errorf("получили %+v, ожидали {Level:%d IntoLevel:%d ToNext:%d}",
					got, c.level, c.into, c.next)
			}
		})
	}
}

// На максимальном уровне расти некуда: ToNext обязан быть нулём, иначе фронт
// нарисует шкалу, которая никогда не заполнится.
func TestLevelForCapsAtMaxLevel(t *testing.T) {
	curve := config.DefaultCurve
	costs, err := config.Costs(curve)
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	total := 0
	for _, cost := range costs {
		total += cost
	}

	for _, xp := range []int{total, total + 1, total * 10} {
		got, levelErr := config.LevelFor(xp, curve)
		if levelErr != nil {
			t.Fatalf("LevelFor(%d): %v", xp, levelErr)
		}
		if got.Level != curve.MaxLevel {
			t.Errorf("опыт %d: уровень %d, ожидали %d", xp, got.Level, curve.MaxLevel)
		}
		if got.ToNext != 0 {
			t.Errorf("опыт %d: ToNext=%d на максимальном уровне", xp, got.ToNext)
		}
	}
}

// Сумма всех переходов обязана приводить ровно на максимальный уровень —
// это связывает Costs и LevelFor между собой, а не проверяет каждую отдельно.
func TestLevelForAgreesWithCostsStepByStep(t *testing.T) {
	curve := config.DefaultCurve
	costs, err := config.Costs(curve)
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	total := 0
	for i, cost := range costs {
		total += cost

		got, levelErr := config.LevelFor(total, curve)
		if levelErr != nil {
			t.Fatalf("LevelFor: %v", levelErr)
		}
		if want := i + 2; got.Level != want {
			t.Fatalf("суммарный опыт %d: уровень %d, ожидали %d", total, got.Level, want)
		}
		if got.IntoLevel != 0 {
			t.Errorf("суммарный опыт %d: IntoLevel=%d, ожидали 0", total, got.IntoLevel)
		}
	}
}

func TestLevelForRejectsBrokenCurve(t *testing.T) {
	_, err := config.LevelFor(100, config.Curve{Base: 0, Factor: 1.25, RoundTo: 10, MaxLevel: 25})
	if !errors.Is(err, config.ErrInvalidCurve) {
		t.Errorf("получили %v, ожидали ErrInvalidCurve", err)
	}
}

// Константы, с которыми сервис уезжает в прод, обязаны быть корректны.
func TestShippedConstantsAreValid(t *testing.T) {
	if err := config.DefaultCurve.Validate(); err != nil {
		t.Errorf("DefaultCurve: %v", err)
	}
	if config.DefaultDailyCareXPCap < 0 {
		t.Errorf("DefaultDailyCareXPCap=%d", config.DefaultDailyCareXPCap)
	}
}
