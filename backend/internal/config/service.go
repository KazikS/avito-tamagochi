// Package config отдаёт справочник экономики: числа, которые фронту запрещено
// хардкодить, потому что при смене баланса клиент и сервер разъедутся молча.
//
// Это первый пакет целевой раскладки internal/<тег>/{handler,service,repo}.go
// (docs/ARCHITECTURE.md). До него слоевые правила линтера не проверяли ничего:
// глобы depguard не совпадали ни с одним файлом в репозитории — что показывал
// `make reconcile` строкой «depguard не матчит ни одного файла».
//
// repo.go здесь нет намеренно. Справочник — константы, а не строки в таблице;
// заводить репозиторий, который ничего не читает, значило бы добавить слой
// ради симметрии. Слоевых правил это не ослабляет: depguard описывает
// handler.go и service.go, и оба здесь настоящие.
package config

import (
	"errors"
	"fmt"
	"math"
)

// maxCost — потолок стоимости одного уровня.
//
// Нужен не для баланса, а чтобы кривая с большим Factor не переполнила int:
// math.Pow вернёт +Inf, а преобразование +Inf в int в Go не определено —
// получилось бы отрицательное или произвольное число порогов вместо ошибки.
const maxCost = 1 << 30

// ErrInvalidCurve — кривая уровней задана бессмысленно.
var ErrInvalidCurve = errors.New("config: некорректная кривая уровней")

// Curve — параметры кривой уровней: стоимость перехода на уровень L равна
// Base * Factor^(L-2), округлённой до RoundTo.
type Curve struct {
	// Base — стоимость перехода с 1-го уровня на 2-й.
	Base int
	// Factor — во сколько раз каждый следующий переход дороже предыдущего.
	Factor float64
	// RoundTo — до чего округляется стоимость, чтобы числа читались человеком.
	RoundTo int
	// MaxLevel — последний достижимый уровень.
	MaxLevel int
}

// DefaultCurve — ПРОВИЗОРНЫЕ значения, взятые из examples в docs/openapi.json.
//
// Это не решение команды. «Кривая уровней» — стоп-вопрос в AGENTS.md
// (25 уровней против 12 почти линейных плюс сезоны), и выбор за людьми.
// Значения вынесены сюда одной структурой ровно для того, чтобы решение
// команды стоило одной правки литерала и не трогало ни одного обработчика.
var DefaultCurve = Curve{
	Base:     100,
	Factor:   1.25,
	RoundTo:  10,
	MaxLevel: 25,
}

// DefaultDailyCareXPCap — ПРОВИЗОРНЫЙ суточный потолок опыта за уход,
// тоже из examples контракта.
//
// Сам потолок — не стоп-вопрос: уход начисляет XP (решено 06.08), а от
// накрутки защищают лог действий и суточный кап (docs/ARCHITECTURE.md,
// инвариант 1). Спорна только величина.
const DefaultDailyCareXPCap = 40

// Validate проверяет, что по кривой вообще можно считать.
func (c Curve) Validate() error {
	switch {
	case c.Base <= 0:
		return fmt.Errorf("%w: Base=%d, ожидалось больше нуля", ErrInvalidCurve, c.Base)
	case c.MaxLevel < 1:
		return fmt.Errorf("%w: MaxLevel=%d, ожидалось не меньше единицы", ErrInvalidCurve, c.MaxLevel)
	case c.RoundTo < 1:
		return fmt.Errorf("%w: RoundTo=%d, ожидалось не меньше единицы", ErrInvalidCurve, c.RoundTo)
	case !(c.Factor > 0) || math.IsInf(c.Factor, 0):
		// Условие написано через «не больше нуля», а не через «<= 0»,
		// чтобы NaN тоже считался некорректным: любое сравнение с NaN ложно.
		return fmt.Errorf("%w: Factor=%v, ожидалось конечное число больше нуля", ErrInvalidCurve, c.Factor)
	}
	return nil
}

// Costs возвращает стоимость каждого перехода: элемент i — сколько опыта
// нужно набрать на уровне i+1, чтобы получить уровень i+2.
//
// Длина результата — MaxLevel-1: с последнего уровня переходить некуда.
// Для некорректной кривой возвращает ошибку, а не «разумные» числа: молча
// подставленный баланс — это разъехавшиеся клиент и сервер.
func Costs(c Curve) ([]int, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	costs := make([]int, 0, c.MaxLevel-1)
	for i := range c.MaxLevel - 1 {
		raw := float64(c.Base) * math.Pow(c.Factor, float64(i))

		// Округление до RoundTo делаем в float, а потом ограничиваем: обратный
		// порядок дал бы переполнение до того, как сработает потолок.
		rounded := math.Round(raw/float64(c.RoundTo)) * float64(c.RoundTo)

		switch {
		case math.IsNaN(rounded):
			return nil, fmt.Errorf("%w: стоимость уровня %d не число", ErrInvalidCurve, i+2)
		case rounded > maxCost:
			costs = append(costs, maxCost)
		case rounded < 1:
			// Округление вниз может дать ноль (Base=1, RoundTo=10): уровень,
			// который берётся бесплатно, сломал бы прогресс — минимум единица.
			costs = append(costs, 1)
		default:
			costs = append(costs, int(rounded))
		}
	}
	return costs, nil
}

// Progress — положение пользователя на кривой.
type Progress struct {
	// Level — текущий уровень, начиная с 1.
	Level int
	// IntoLevel — опыт, накопленный внутри текущего уровня.
	IntoLevel int
	// ToNext — сколько опыта до следующего уровня; 0 на максимальном.
	ToNext int
}

// LevelFor переводит суммарный опыт в уровень и прогресс внутри него.
//
// Отрицательный опыт считается нулевым: это не вход пользователя, а следствие
// возможной ошибки в подсчёте выше по стеку, и падать здесь нечем.
func LevelFor(totalXP int, c Curve) (Progress, error) {
	costs, err := Costs(c)
	if err != nil {
		return Progress{}, err
	}

	if totalXP < 0 {
		totalXP = 0
	}

	level := 1
	left := totalXP
	for _, cost := range costs {
		if left < cost {
			return Progress{Level: level, IntoLevel: left, ToNext: cost - left}, nil
		}
		left -= cost
		level++
	}

	// Максимальный уровень: расти некуда, остаток опыта показываем как есть.
	return Progress{Level: level, IntoLevel: left, ToNext: 0}, nil
}
