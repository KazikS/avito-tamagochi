// Package pet — домен питомца: распад показателей, уход, настроение и опыт.
//
// Это слой service.go целевой раскладки (docs/ARCHITECTURE.md): чистые
// функции, время параметром, никакого HTTP, Gin и SQL. Правила проверяет
// depguard (service-layer) и forbidigo (time.Now), а не ревью.
//
// # Почему показатели хранятся базой, а не «текущим значением»
//
// Контракт описывает Pet.updatedAt как «момент, на который посчитан decay;
// точка отсчёта для следующего пересчёта». Отсюда единственная корректная
// схема: в базе лежит снимок показателей и момент этого снимка, а текущее
// состояние ВЫЧИСЛЯЕТСЯ из них при каждом чтении. Запись происходит только
// когда состояние действительно меняется — то есть при действии ухода.
//
// Альтернатива — дописывать в базу пересчитанное значение на каждом GET —
// выглядит эквивалентной и таковой не является: показатели целые, при каждой
// записи результат округляется и теряет дробную часть, и питомец «худеет»
// тем быстрее, чем чаще пользователь открывает экран. Наблюдение меняло бы
// наблюдаемое. Инвариант
// «чтение не меняет состояние» держится тем, что Decay ничего не пишет, и
// закреплён тестом TestReadingDoesNotAgePet.
package pet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"tamagochi/internal/config"
	"tamagochi/pkg/clock"
)

// StatKey — показатель питомца. Значения дословно из перечисления StatKey
// в docs/openapi.json: клиент присылает и получает именно эти строки.
type StatKey string

const (
	// StatHunger — сытость.
	StatHunger StatKey = "hunger"
	// StatJoy — радость.
	StatJoy StatKey = "joy"
	// StatClean — чистота.
	StatClean StatKey = "clean"
	// StatEnergy — энергия.
	StatEnergy StatKey = "energy"
)

// StatKeys — все показатели в порядке, в котором их показывает интерфейс.
// Отдельный список, а не обход map: у map порядок не определён, и ответ
// сервера менялся бы от запроса к запросу.
var StatKeys = []StatKey{StatHunger, StatJoy, StatClean, StatEnergy}

// statMin и statMax — жёсткие границы показателя из контракта: «Целые 0..100.
// Потолок жёсткий: 82 + 30 = 100, остаток сгорает».
const (
	statMin = 0
	statMax = 100
)

// Stats — четыре показателя питомца, целые в диапазоне 0..100.
type Stats struct {
	Hunger int
	Joy    int
	Clean  int
	Energy int
}

// Get возвращает значение показателя.
func (s Stats) Get(k StatKey) (int, error) {
	switch k {
	case StatHunger:
		return s.Hunger, nil
	case StatJoy:
		return s.Joy, nil
	case StatClean:
		return s.Clean, nil
	case StatEnergy:
		return s.Energy, nil
	default:
		return 0, fmt.Errorf("%w: показатель %q", ErrUnknownStat, string(k))
	}
}

// with возвращает копию показателей с изменённым k. Значение зажимается в
// 0..100 здесь, а не у вызывающего: потолок — свойство самого показателя.
func (s Stats) with(k StatKey, v int) (Stats, error) {
	v = clampStat(v)
	switch k {
	case StatHunger:
		s.Hunger = v
	case StatJoy:
		s.Joy = v
	case StatClean:
		s.Clean = v
	case StatEnergy:
		s.Energy = v
	default:
		return Stats{}, fmt.Errorf("%w: показатель %q", ErrUnknownStat, string(k))
	}
	return s, nil
}

// Min возвращает минимальный из показателей. Настроение — производная именно
// от минимума (так сказано в контракте у PetMood), поэтому это не помощник
// «на всякий случай», а часть доменного правила.
func (s Stats) Min() int {
	m := s.Hunger
	for _, v := range []int{s.Joy, s.Clean, s.Energy} {
		if v < m {
			m = v
		}
	}
	return m
}

// Valid сообщает, что все показатели лежат в допустимом диапазоне.
func (s Stats) Valid() bool {
	for _, v := range []int{s.Hunger, s.Joy, s.Clean, s.Energy} {
		if v < statMin || v > statMax {
			return false
		}
	}
	return true
}

// Mood — настроение питомца. Значения из перечисления PetMood контракта.
type Mood string

const (
	// MoodRadiant — сияющий.
	MoodRadiant Mood = "radiant"
	// MoodHappy — довольный.
	MoodHappy Mood = "happy"
	// MoodNeutral — обычный.
	MoodNeutral Mood = "neutral"
	// MoodSad — грустный.
	MoodSad Mood = "sad"
	// MoodSick — больной.
	MoodSick Mood = "sick"
	// MoodSleeping — спит; настроение не считается по показателям.
	MoodSleeping Mood = "sleeping"
)

// ActionKind — действие ухода. Значения из перечисления PetActionKind.
type ActionKind string

const (
	// ActionFeed — покормить.
	ActionFeed ActionKind = "feed"
	// ActionPlay — поиграть.
	ActionPlay ActionKind = "play"
	// ActionWash — помыть.
	ActionWash ActionKind = "wash"
	// ActionSleep — уложить спать.
	ActionSleep ActionKind = "sleep"
	// ActionWake — разбудить.
	ActionWake ActionKind = "wake"
)

// ActionKinds — все действия в порядке показа.
var ActionKinds = []ActionKind{ActionFeed, ActionPlay, ActionWash, ActionSleep, ActionWake}

// Доменные ошибки. Обёрнуты через %w, чтобы транспорт различал их
// errors.Is и переводил в коды контракта, а не разбирал текст.
var (
	// ErrUnknownStat — показателя с таким ключом не существует.
	ErrUnknownStat = errors.New("pet: неизвестный показатель")
	// ErrUnknownAction — действия с таким ключом не существует.
	ErrUnknownAction = errors.New("pet: неизвестное действие")
	// ErrInvalidEconomy — таблицы экономики заданы бессмысленно.
	ErrInvalidEconomy = errors.New("pet: некорректная экономика")
	// ErrAlreadyAsleep — питомец уже спит.
	ErrAlreadyAsleep = errors.New("pet: питомец уже спит")
	// ErrNotAsleep — питомец не спит.
	ErrNotAsleep = errors.New("pet: питомец не спит")
	// ErrAsleep — действие требует бодрствующего питомца.
	ErrAsleep = errors.New("pet: питомец спит")
)

// Action — что делает одно действие ухода.
type Action struct {
	// Stat — какой показатель поднимает. Пусто у sleep и wake: они меняют
	// не показатель, а режим.
	Stat StatKey
	// Effect — на сколько поднимает показатель.
	Effect int
	// XP — базовый опыт до множителей.
	XP int
	// DailyLimit — сколько раз за сутки действие вообще доступно.
	DailyLimit int
}

// MoodTier — порог настроения: начиная с какого минимального показателя
// действует это настроение и какой множитель опыта даёт.
type MoodTier struct {
	// Mood — настроение.
	Mood Mood
	// MinStat — нижняя граница минимального показателя, включительно.
	MinStat int
	// Multiplier — множитель опыта в этом настроении.
	Multiplier float64
}

// Economy — все числа домена в одном месте.
//
// Значения ПРОВИЗОРНЫЕ, ровно как config.DefaultCurve: контракт описывает
// форму (Config.stats, Config.actions, Config.moods), но конкретных чисел не
// задаёт. Собраны одной структурой, чтобы решение команды по балансу стоило
// правки литерала и не трогало ни одного обработчика, и чтобы GET /config мог
// отдать их фронту — тому запрещено хардкодить экономику.
type Economy struct {
	// DecayPerHour — сколько единиц показателя теряется за час бодрствования.
	DecayPerHour map[StatKey]float64
	// SleepEnergyPerHour — сколько энергии восстанавливается за час сна.
	SleepEnergyPerHour float64
	// Actions — таблица действий ухода.
	Actions map[ActionKind]Action
	// MoodTiers — пороги настроения, от лучшего к худшему.
	MoodTiers []MoodTier
}

// DefaultEconomy — провизорный баланс.
//
// Подобран так, чтобы сутки без захода ощутимо роняли питомца, но не убивали:
// голод 4/час — это −96 за сутки, то есть зашедший раз в день пользователь
// застаёт питомца живым, а пропустивший день — почти на нуле. Это и есть
// «повод вернуться завтра» из кейса, выраженный числом.
var DefaultEconomy = Economy{
	DecayPerHour: map[StatKey]float64{
		StatHunger: 4,
		StatJoy:    3,
		StatClean:  2,
		StatEnergy: 3,
	},
	SleepEnergyPerHour: 12,
	Actions: map[ActionKind]Action{
		ActionFeed:  {Stat: StatHunger, Effect: 30, XP: 10, DailyLimit: 6},
		ActionPlay:  {Stat: StatJoy, Effect: 25, XP: 12, DailyLimit: 6},
		ActionWash:  {Stat: StatClean, Effect: 30, XP: 10, DailyLimit: 4},
		ActionSleep: {XP: 5, DailyLimit: 2},
		ActionWake:  {XP: 0, DailyLimit: 4},
	},
	MoodTiers: []MoodTier{
		{Mood: MoodRadiant, MinStat: 80, Multiplier: 1.25},
		{Mood: MoodHappy, MinStat: 60, Multiplier: 1.10},
		{Mood: MoodNeutral, MinStat: 40, Multiplier: 1.00},
		{Mood: MoodSad, MinStat: 20, Multiplier: 0.90},
		{Mood: MoodSick, MinStat: 0, Multiplier: 0.75},
	},
}

// Validate проверяет, что по экономике можно считать.
//
// Вызывается при сборке обработчика, а не в запросе: кривая экономики
// приходит из констант сборки, и ошибка в ней — неработающий сервис, а не
// плохой запрос пользователя.
func (e Economy) Validate() error {
	for _, k := range StatKeys {
		rate, ok := e.DecayPerHour[k]
		switch {
		case !ok:
			return fmt.Errorf("%w: нет скорости распада для %q", ErrInvalidEconomy, string(k))
		case math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0:
			return fmt.Errorf("%w: скорость распада %q = %v, ожидалось конечное число не меньше нуля",
				ErrInvalidEconomy, string(k), rate)
		}
	}

	if r := e.SleepEnergyPerHour; math.IsNaN(r) || math.IsInf(r, 0) || r < 0 {
		return fmt.Errorf("%w: восстановление энергии во сне %v, ожидалось конечное число не меньше нуля",
			ErrInvalidEconomy, r)
	}

	for _, k := range ActionKinds {
		a, ok := e.Actions[k]
		switch {
		case !ok:
			return fmt.Errorf("%w: нет действия %q", ErrInvalidEconomy, string(k))
		case a.XP < 0:
			return fmt.Errorf("%w: опыт действия %q = %d, ожидалось не меньше нуля",
				ErrInvalidEconomy, string(k), a.XP)
		case a.DailyLimit < 0:
			return fmt.Errorf("%w: суточный лимит действия %q = %d, ожидалось не меньше нуля",
				ErrInvalidEconomy, string(k), a.DailyLimit)
		}
		// Действие, поднимающее показатель, обязано поднимать его на
		// положительную величину: нулевой эффект — это кнопка, которая
		// ничего не делает, но начисляет опыт.
		if a.Stat != "" {
			if _, err := (Stats{}).Get(a.Stat); err != nil {
				return fmt.Errorf("%w: действие %q ссылается на %w", ErrInvalidEconomy, string(k), err)
			}
			if a.Effect <= 0 {
				return fmt.Errorf("%w: эффект действия %q = %d, ожидалось больше нуля",
					ErrInvalidEconomy, string(k), a.Effect)
			}
		}
	}

	if len(e.MoodTiers) == 0 {
		return fmt.Errorf("%w: не задано ни одного порога настроения", ErrInvalidEconomy)
	}
	// Пороги обязаны убывать и заканчиваться нулём, иначе у минимального
	// показателя 0 не найдётся настроения и MoodOf вернёт пустую строку —
	// значение, которого нет в перечислении контракта.
	prev := statMax + 1
	for i, t := range e.MoodTiers {
		switch {
		case t.MinStat >= prev:
			return fmt.Errorf("%w: пороги настроения не убывают: %d после %d", ErrInvalidEconomy, t.MinStat, prev)
		case math.IsNaN(t.Multiplier) || math.IsInf(t.Multiplier, 0) || t.Multiplier < 0:
			return fmt.Errorf("%w: множитель настроения %q = %v, ожидалось конечное число не меньше нуля",
				ErrInvalidEconomy, string(t.Mood), t.Multiplier)
		}
		prev = t.MinStat
		if i == len(e.MoodTiers)-1 && t.MinStat != statMin {
			return fmt.Errorf("%w: последний порог настроения %d, ожидался %d", ErrInvalidEconomy, t.MinStat, statMin)
		}
	}
	return nil
}

// Decay считает показатели на момент to, если на момент from они были s.
//
// Сигнатура ровно та, что записана в сообщении forbidigo: decay(state, from, to).
// Время — параметр, часы сюда не приходят.
//
// Функция ничего не пишет и вызывается на каждом чтении: см. преамбулу пакета
// о том, почему пересчитанное значение нельзя сохранять обратно.
func Decay(s Stats, sleeping bool, from, to time.Time, e Economy) Stats {
	hours := to.Sub(from).Hours()
	// Часы, идущие назад, не лечат питомца. Отрицательный интервал бывает при
	// расхождении часов на разных узлах и при сдвиге часов демо-стенда назад;
	// «отрицательный распад» превратил бы его в способ накрутки.
	if !(hours > 0) {
		return s
	}

	out := s
	for _, k := range StatKeys {
		// Во сне энергия восстанавливается вместо того, чтобы падать.
		if sleeping && k == StatEnergy {
			continue
		}
		cur, err := s.Get(k)
		if err != nil {
			// Ключ из StatKeys всегда известен Get: ветка недостижима, но
			// молча пропускать показатель лучше, чем обнулить остальные.
			continue
		}
		// Округление вниз одно и на весь интервал, а не по шагам: пошаговое
		// округление даёт разный результат при разной частоте пересчёта.
		next, err := out.with(k, roundToStat(float64(cur)-e.DecayPerHour[k]*hours))
		if err != nil {
			continue
		}
		// Присваиваем только после успеха: with возвращает нулевые Stats
		// вместе с ошибкой, и `out, err = out.with(...)` обнулил бы все
		// показатели разом.
		out = next
	}

	if sleeping {
		if next, err := out.with(StatEnergy, roundToStat(float64(s.Energy)+e.SleepEnergyPerHour*hours)); err == nil {
			out = next
		}
	}
	return out
}

// roundToStat округляет до ближайшего целого и зажимает в 0..100, ограничивая
// ЕЩЁ ВО FLOAT.
//
// Порядок важен ровно по той же причине, что и в config.Costs: преобразование
// float64 в int вне диапазона типа в Go не определено, поэтому потолок надо
// применять до преобразования, а не после.
//
// Округление вниз (было раньше) — не требование контракта: «округление вниз»
// в контракте сказано ровно один раз и ровно про формулу XP, не про распад
// показателей. С floor любой, сколь угодно малый положительный интервал
// снимал 1 очко: floor(100 − ε) = 99 для любого ε>0, то есть питомец переставал
// показывать «100» практически сразу после создания — воспроизведено вручную
// через реальный HTTP-запрос (curl сразу после POST /pets), не найдено
// юнит-тестами, потому что все они двигали часы на целые интервалы.
func roundToStat(v float64) int {
	if math.IsNaN(v) {
		return statMin
	}
	v = math.Round(v)
	if v < statMin {
		return statMin
	}
	if v > statMax {
		return statMax
	}
	return int(v)
}

// MoodOf возвращает настроение и его множитель опыта.
//
// Спящий питомец имеет настроение sleeping с нейтральным множителем: контракт
// перечисляет sleeping наравне с остальными, а считать «радость» у спящего
// незачем.
func MoodOf(s Stats, sleeping bool, e Economy) (Mood, float64) {
	if sleeping {
		return MoodSleeping, 1
	}
	m := s.Min()
	for _, t := range e.MoodTiers {
		if m >= t.MinStat {
			return t.Mood, t.Multiplier
		}
	}
	// Недостижимо при пройденной Economy.Validate: последний порог равен 0,
	// а показатель не бывает отрицательным. Оставлено как честный отказ, а не
	// как пустая строка, которой нет в перечислении контракта.
	return MoodSick, 0
}

// Outcome — результат одного действия ухода над состоянием.
type Outcome struct {
	// Stats — показатели после действия.
	Stats Stats
	// Sleeping — режим сна после действия.
	Sleeping bool
	// StatCapped — показатель был уже 100 ДО действия.
	//
	// Контракт: «XP не начислен, анимацию всё равно играем». То есть это не
	// ошибка запроса, а исход: 200 и честный ноль опыта.
	StatCapped bool
	// BaseXP — базовый опыт действия до множителей и до суточного капа.
	BaseXP int
}

// ApplyAction применяет действие ухода к состоянию.
//
// Возвращает исход, а не ошибку, когда показатель уже полон: «покормить сытого»
// — это разрешённое действие с нулевым опытом, так написано в контракте.
// Ошибка остаётся для того, что сделать нельзя в принципе: разбудить
// бодрствующего, покормить спящего.
func ApplyAction(kind ActionKind, s Stats, sleeping bool, e Economy) (Outcome, error) {
	a, ok := e.Actions[kind]
	if !ok {
		return Outcome{}, fmt.Errorf("%w: %q", ErrUnknownAction, string(kind))
	}

	switch kind {
	case ActionSleep:
		if sleeping {
			return Outcome{}, ErrAlreadyAsleep
		}
		return Outcome{Stats: s, Sleeping: true, BaseXP: a.XP}, nil

	case ActionWake:
		if !sleeping {
			return Outcome{}, ErrNotAsleep
		}
		return Outcome{Stats: s, Sleeping: false, BaseXP: a.XP}, nil

	case ActionFeed, ActionPlay, ActionWash:
		// Ухаживать за спящим нельзя: иначе сон становится бесплатным
		// множителем — энергия растёт, а остальные показатели поднимаются
		// кормлением, и «уложить спать» превращается в оптимальную стратегию.
		if sleeping {
			return Outcome{}, ErrAsleep
		}
		cur, err := s.Get(a.Stat)
		if err != nil {
			return Outcome{}, err
		}
		if cur >= statMax {
			// Показатель был полон ДО действия — опыт не начисляем.
			return Outcome{Stats: s, Sleeping: sleeping, StatCapped: true, BaseXP: 0}, nil
		}
		next, err := s.with(a.Stat, cur+a.Effect)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Stats: next, Sleeping: sleeping, BaseXP: a.XP}, nil

	default:
		return Outcome{}, fmt.Errorf("%w: %q", ErrUnknownAction, string(kind))
	}
}

// Award — сколько опыта реально начислено за действие.
type Award struct {
	// Granted — начисленный опыт, уже с множителями и суточным капом.
	Granted int
	// CapReached — суточный потолок ухода исчерпан после этого начисления.
	CapReached bool
}

// AwardXP считает опыт за действие ухода.
//
// Порядок множителей дословно из контракта: «XP = базовый × множитель_настроения
// × множитель_стрика. Настроение берётся ПОСЛЕ изменения показателя.
// Округление вниз до целого».
//
// Суточный потолок обрезает последнее действие ЧАСТИЧНО — тоже требование
// контракта: «осталось 5 из 40 — начисляем 5, не 0».
func AwardXP(baseXP int, moodMul, streakMul float64, spentToday, dailyCap int) (Award, error) {
	switch {
	case baseXP < 0:
		return Award{}, fmt.Errorf("%w: базовый опыт %d, ожидалось не меньше нуля", ErrInvalidEconomy, baseXP)
	case dailyCap < 0:
		return Award{}, fmt.Errorf("%w: суточный кап %d, ожидалось не меньше нуля", ErrInvalidEconomy, dailyCap)
	case spentToday < 0:
		return Award{}, fmt.Errorf("%w: потрачено за сутки %d, ожидалось не меньше нуля", ErrInvalidEconomy, spentToday)
	case badMultiplier(moodMul):
		return Award{}, fmt.Errorf("%w: множитель настроения %v", ErrInvalidEconomy, moodMul)
	case badMultiplier(streakMul):
		return Award{}, fmt.Errorf("%w: множитель стрика %v", ErrInvalidEconomy, streakMul)
	}

	// Округление вниз одно, после обоих множителей: floor(floor(a*b)*c) даёт
	// другое число, а контракт задаёт одно произведение и одно округление.
	want := math.Floor(float64(baseXP) * moodMul * streakMul)
	// Произведение больших множителей может уйти за пределы int; сравнение
	// делаем во float, до преобразования.
	if want > float64(dailyCap) {
		want = float64(dailyCap)
	}
	granted := int(want)

	remaining := dailyCap - spentToday
	if remaining < 0 {
		remaining = 0
	}
	if granted > remaining {
		granted = remaining
	}

	return Award{
		Granted:    granted,
		CapReached: spentToday+granted >= dailyCap,
	}, nil
}

// badMultiplier отсеивает множители, на которые нельзя умножать.
func badMultiplier(m float64) bool {
	return math.IsNaN(m) || math.IsInf(m, 0) || m < 0
}

// clampStat зажимает значение показателя в 0..100.
func clampStat(v int) int {
	if v < statMin {
		return statMin
	}
	if v > statMax {
		return statMax
	}
	return v
}

// --- Прикладной слой --------------------------------------------------------
//
// Ниже — сборка чистых функций выше в сценарии. Хранилище здесь конкретное
// (*Repo), а не интерфейс: правило проекта — не заводить интерфейс, пока нет
// второй реализации, и здесь её нет. Тестируемость от этого не страдает,
// потому что вся арифметика живёт в чистых функциях выше и проверяется без
// базы, а то, ради чего база нужна (однократность при повторе и при гонке),
// фейком не проверяется в принципе — см. преамбулу pkg/pgtest.

// ErrDailyLimit — суточный лимит самого действия исчерпан.
//
// Это не то же самое, что суточный кап опыта: кап обнуляет начисление, но
// действие остаётся доступным, а лимит запрещает само действие.
var ErrDailyLimit = errors.New("pet: суточный лимит действия исчерпан")

// Actor — от чьего имени выполняется сценарий.
type Actor struct {
	// UserID — владелец питомца.
	UserID uuid.UUID
	// Location — таймзона аккаунта. По ней считаются сутки: контракт,
	// правило 6 — «Сутки и стрик считаются по timezone аккаунта, а не по
	// времени устройства». Пока аккаунтов нет, вызывающий передаёт UTC;
	// когда приедет авторизация, сюда придёт таймзона из профиля.
	Location *time.Location
}

// day возвращает календарные сутки в таймзоне аккаунта.
func (a Actor) day(now time.Time) time.Time {
	loc := a.Location
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Service собирает сценарии питомца.
type Service struct {
	repo     *Repo
	clock    clock.Clock
	economy  Economy
	curve    config.Curve
	dailyCap int
}

// NewService собирает сервис и отказывается собираться на бессмысленных
// константах: ошибка в экономике — это неработающий сервис, и падать он
// должен на старте, а не на первом действии пользователя.
func NewService(repo *Repo, c clock.Clock, e Economy, curve config.Curve, dailyCap int) (*Service, error) {
	switch {
	case repo == nil:
		return nil, errors.New("pet: нет репозитория")
	case c == nil:
		return nil, errors.New("pet: нет часов")
	case dailyCap < 0:
		return nil, fmt.Errorf("%w: суточный кап опыта %d", ErrInvalidEconomy, dailyCap)
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if err := curve.Validate(); err != nil {
		return nil, err
	}
	return &Service{repo: repo, clock: c, economy: e, curve: curve, dailyCap: dailyCap}, nil
}

// Availability — доступность одного действия на сегодня.
type Availability struct {
	// Remaining — сколько раз ещё можно сегодня.
	Remaining int `json:"remaining"`
	// XPCapped — суточный потолок опыта ухода исчерпан.
	XPCapped bool `json:"xpCapped"`
}

// View — состояние питомца, каким его видит клиент.
//
// Всё производное (настроение, уровень, стадия, доступность действий)
// посчитано сервером: контракт запрещает клиенту вычислять это самому.
type View struct {
	ID             uuid.UUID                   `json:"id"`
	PresetID       string                      `json:"presetId"`
	Name           string                      `json:"name"`
	Stats          Stats                       `json:"stats"`
	Sleeping       bool                        `json:"sleeping"`
	Mood           Mood                        `json:"mood"`
	MoodMultiplier float64                     `json:"moodMultiplier"`
	Level          int                         `json:"level"`
	XP             int                         `json:"xp"`
	XPToNext       int                         `json:"xpToNext"`
	TotalXP        int                         `json:"totalXp"`
	Stage          int                         `json:"stage"`
	StageLabel     string                      `json:"stageLabel"`
	Actions        map[ActionKind]Availability `json:"actions"`
	UpdatedAt      time.Time                   `json:"updatedAt"`
}

// ActResult — результат действия ухода.
//
// Сериализуется целиком и хранится в журнале: повтор обязан вернуть ПРЕЖНИЙ
// результат, а не пересчитанный на новое время.
type ActResult struct {
	Pet        View `json:"pet"`
	XPGained   int  `json:"xpGained"`
	StatCapped bool `json:"statCapped"`
	LeveledUp  bool `json:"leveledUp"`
	// StageChanged — стадия питомца изменилась этим действием. Не в
	// контракте (docs/openapi.json → ActionResult) — добавлено для WS-события
	// level.up, чьё поле stageChanged есть в docs/openapi.json → x-websocket.
	StageChanged     bool `json:"stageChanged,omitempty"`
	CareXPToday      int  `json:"careXpToday"`
	CareXPCapReached bool `json:"careXpCapReached"`
}

// stageTiers — с какого уровня начинается стадия. Стадий четыре, как в
// перечислении контракта (Pet.stage: 1..4).
var stageTiers = []struct {
	FromLevel int
	Stage     int
	Label     string
}{
	{FromLevel: 18, Stage: 4, Label: "Хранитель"},
	{FromLevel: 10, Stage: 3, Label: "Знаток"},
	{FromLevel: 5, Stage: 2, Label: "Искатель"},
	{FromLevel: 1, Stage: 1, Label: "Новичок"},
}

// StageFor переводит уровень в стадию и её название.
func StageFor(level int) (int, string) {
	for _, t := range stageTiers {
		if level >= t.FromLevel {
			return t.Stage, t.Label
		}
	}
	// Уровень меньше первого порога бывает только при испорченных данных;
	// первая стадия честнее, чем ноль, которого нет в перечислении.
	return stageTiers[len(stageTiers)-1].Stage, stageTiers[len(stageTiers)-1].Label
}

// view собирает состояние питомца на момент now.
//
// Ничего не пишет: показатели пересчитываются при каждом чтении из снимка,
// см. преамбулу пакета.
func (s *Service) view(rec Record, now time.Time, counts map[ActionKind]int, spentToday int) (View, error) {
	cur := Decay(rec.Stats, rec.Sleeping, rec.StatsAt, now, s.economy)
	mood, mul := MoodOf(cur, rec.Sleeping, s.economy)

	progress, err := config.LevelFor(rec.TotalXP, s.curve)
	if err != nil {
		return View{}, fmt.Errorf("pet: уровень по опыту %d: %w", rec.TotalXP, err)
	}
	stage, label := StageFor(progress.Level)

	capped := spentToday >= s.dailyCap
	actions := make(map[ActionKind]Availability, len(ActionKinds))
	for _, k := range ActionKinds {
		remaining := s.economy.Actions[k].DailyLimit - counts[k]
		if remaining < 0 {
			remaining = 0
		}
		actions[k] = Availability{Remaining: remaining, XPCapped: capped}
	}

	return View{
		ID:             rec.ID,
		PresetID:       rec.PresetID,
		Name:           rec.Name,
		Stats:          cur,
		Sleeping:       rec.Sleeping,
		Mood:           mood,
		MoodMultiplier: mul,
		Level:          progress.Level,
		XP:             progress.IntoLevel,
		XPToNext:       progress.ToNext,
		TotalXP:        rec.TotalXP,
		Stage:          stage,
		StageLabel:     label,
		Actions:        actions,
		UpdatedAt:      now,
	}, nil
}

// Create заводит питомца пользователю. Второй питомец — ErrPetExists.
func (s *Service) Create(ctx context.Context, a Actor, presetID, name string) (View, error) {
	now := s.clock.Now()

	rec := Record{
		ID:       uuid.New(),
		UserID:   a.UserID,
		PresetID: presetID,
		Name:     name,
		// Новый питомец полон: пустые показатели на старте означали бы, что
		// пользователь видит больного питомца в первую же секунду.
		Stats:   Stats{Hunger: statMax, Joy: statMax, Clean: statMax, Energy: statMax},
		StatsAt: now,
	}
	if err := s.repo.Create(ctx, rec); err != nil {
		return View{}, err
	}
	return s.view(rec, now, nil, 0)
}

// Get возвращает состояние питомца.
func (s *Service) Get(ctx context.Context, a Actor) (View, error) {
	now := s.clock.Now()
	day := a.day(now)

	rec, err := s.repo.ByUser(ctx, a.UserID)
	if err != nil {
		return View{}, err
	}
	counts, err := s.repo.CountsToday(ctx, a.UserID, day)
	if err != nil {
		return View{}, err
	}
	spent, err := s.repo.SpentToday(ctx, a.UserID, day)
	if err != nil {
		return View{}, err
	}
	return s.view(rec, now, counts, spent)
}

// Act выполняет действие ухода.
//
// actionID приходит от клиента и делает вызов идемпотентным: повтор с тем же
// идентификатором возвращает прежний результат и не начисляет опыт второй раз.
//
// replayed=true означает, что это повтор уже выполненного actionID: Result —
// прежний ответ, а не следствие нового изменения. Вызывающий (ws.go) обязан
// не рассылать события реального времени на повтор: контракт требует слать
// pet.stats «только при реальном изменении», а на повторе его нет.
func (s *Service) Act(ctx context.Context, a Actor, actionID uuid.UUID, kind ActionKind) (result ActResult, replayed bool, err error) {
	now := s.clock.Now()
	day := a.day(now)

	if _, ok := s.economy.Actions[kind]; !ok {
		return ActResult{}, false, fmt.Errorf("%w: %q", ErrUnknownAction, string(kind))
	}

	raw, replayed, err := s.repo.ApplyAction(ctx, a.UserID, actionID, kind, day, func(snap Snapshot) (Decision, error) {
		return s.decide(snap, now, kind)
	})
	if err != nil {
		return ActResult{}, false, err
	}

	var out ActResult
	if unmarshalErr := json.Unmarshal(raw, &out); unmarshalErr != nil {
		return ActResult{}, false, fmt.Errorf("pet: разбор сохранённого результата действия: %w", unmarshalErr)
	}
	return out, replayed, nil
}

// decide — доменное решение по одному действию. Чистое относительно базы:
// получает снимок, возвращает, что записать.
func (s *Service) decide(snap Snapshot, now time.Time, kind ActionKind) (Decision, error) {
	action := s.economy.Actions[kind]
	if snap.CountsToday[kind] >= action.DailyLimit {
		return Decision{}, fmt.Errorf("%w: %q, сделано %d из %d",
			ErrDailyLimit, string(kind), snap.CountsToday[kind], action.DailyLimit)
	}

	// Распад досчитывается ДО действия: иначе кормление «отменяло» бы часы
	// простоя, и питомец, к которому не заходили сутки, оказывался бы сытым
	// от одного нажатия.
	cur := Decay(snap.Pet.Stats, snap.Pet.Sleeping, snap.Pet.StatsAt, now, s.economy)

	outcome, err := ApplyAction(kind, cur, snap.Pet.Sleeping, s.economy)
	if err != nil {
		return Decision{}, err
	}

	// Настроение берётся ПОСЛЕ изменения показателя — так требует контракт,
	// правило 3.
	_, moodMul := MoodOf(outcome.Stats, outcome.Sleeping, s.economy)

	// Множитель стрика — единица: серия дней ещё не реализована. Это
	// осознанная единица, а не забытый множитель: как только появится streak,
	// сюда придёт его коэффициент, и порядок умножения уже правильный.
	const streakMultiplier = 1.0

	award, err := AwardXP(outcome.BaseXP, moodMul, streakMultiplier, snap.SpentToday, s.dailyCap)
	if err != nil {
		return Decision{}, err
	}

	before, err := config.LevelFor(snap.Pet.TotalXP, s.curve)
	if err != nil {
		return Decision{}, fmt.Errorf("pet: уровень до действия: %w", err)
	}
	updated := snap.Pet
	updated.Stats = outcome.Stats
	updated.Sleeping = outcome.Sleeping
	updated.StatsAt = now
	updated.TotalXP = snap.Pet.TotalXP + award.Granted

	after, err := config.LevelFor(updated.TotalXP, s.curve)
	if err != nil {
		return Decision{}, fmt.Errorf("pet: уровень после действия: %w", err)
	}

	counts := make(map[ActionKind]int, len(snap.CountsToday)+1)
	for k, v := range snap.CountsToday {
		counts[k] = v
	}
	counts[kind]++
	spent := snap.SpentToday + award.Granted

	v, err := s.view(updated, now, counts, spent)
	if err != nil {
		return Decision{}, err
	}

	beforeStage, _ := StageFor(before.Level)
	afterStage, _ := StageFor(after.Level)

	result, err := json.Marshal(ActResult{
		Pet:              v,
		XPGained:         award.Granted,
		StatCapped:       outcome.StatCapped,
		LeveledUp:        after.Level > before.Level,
		StageChanged:     afterStage != beforeStage,
		CareXPToday:      spent,
		CareXPCapReached: award.CapReached,
	})
	if err != nil {
		return Decision{}, fmt.Errorf("pet: сериализация результата действия: %w", err)
	}

	return Decision{
		Stats:     updated.Stats,
		Sleeping:  updated.Sleeping,
		StatsAt:   updated.StatsAt,
		XPGranted: award.Granted,
		Result:    result,
	}, nil
}
