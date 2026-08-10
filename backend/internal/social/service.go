// Package social — лидерборд. Тег контракта "social".
//
// Реализована ЧАСТЬ /leaderboard, явно, не молча: только scope=top (глобальный
// топ). scope=league требует концепцию лиг (промоушен/релегейшн, окончание
// сезона) и scope=friends — граф друзей; ни того, ни другого в проекте нет.
// Запрос с этими scope отвечает VALIDATIONERROR, а не тихо подменяется на top.
//
// Ранжирование — по опыту за 7 дней (дословно контракт: «Ранжирование по XP
// за 7 дней, тай-брейк — длина стрика»). Тай-брейк здесь — user_id, не стрик:
// стрика в проекте ещё нет (см. AGENTS.md → стоп-вопросы нет, но фичи streak
// нет в коде — docs/RECONCILIATION.md). Streak в ответе — всегда 0, honest
// zero, а не выдуманное число.
//
// Nickname — временно имя питомца, а не профиля: настоящих ников нет,
// потому что feat/auth (модель User) не смержена. Это провизорная замена
// того же класса, что DefaultCurve в internal/config — заменить одной
// строкой маппинга, когда придёт профиль пользователя.
package social

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"tamagochi/internal/config"
	"tamagochi/internal/pet"
	"tamagochi/pkg/clock"
)

// DefaultLimit и MaxLimit — дословно контракт: default 25, maximum 50.
const (
	DefaultLimit = 25
	MaxLimit     = 50
)

// ErrUnsupportedScope — запрошен scope, для которого нет данных (league, friends).
var ErrUnsupportedScope = errors.New("social: scope не поддерживается")

// ErrBadCursor — курсор не разбирается: не наш формат или испорчен на клиенте.
var ErrBadCursor = errors.New("social: некорректный курсор")

// ErrUnknownScope — значение не входит в перечисление контракта вообще
// (опечатка, пустая строка, случайная строка). Отдельно от ErrUnsupportedScope
// — тот про значение, которое контракт знает, а этот срез ещё не реализует;
// смешивать их в один код значило бы одинаково отвечать на «такого scope не
// существует» и «это не твоя вина, просто ещё не сделано».
var ErrUnknownScope = errors.New("social: неизвестный scope")

// Scope — какой лидерборд запрошен.
type Scope string

const (
	ScopeTop     Scope = "top"
	ScopeLeague  Scope = "league"
	ScopeFriends Scope = "friends"
)

// ParseScope разбирает scope из query-параметра.
func ParseScope(raw string) (Scope, error) {
	switch Scope(raw) {
	case ScopeTop:
		return ScopeTop, nil
	case ScopeLeague, ScopeFriends:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedScope, raw)
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownScope, raw)
	}
}

// NormalizeLimit применяет дефолт и потолок контракта.
func NormalizeLimit(requested *int) int {
	if requested == nil || *requested <= 0 {
		return DefaultLimit
	}
	if *requested > MaxLimit {
		return MaxLimit
	}
	return *requested
}

// Row — строка рейтинга, как её вернул repo.go: ROW_NUMBER уже посчитан в SQL,
// потому что ранг — это глобальная позиция, а не позиция внутри страницы, и
// пересчитывать его в Go по одной странице означало бы считать неверно.
type Row struct {
	UserID   uuid.UUID
	PresetID string
	Name     string
	TotalXP  int
	WeeklyXP int
	Rank     int
}

// Entry — строка лидерборда, как её видит клиент (без JSON-тегов контракта —
// это забота handler.go, ровно как и у Stats/View в internal/pet).
type Entry struct {
	UserID   uuid.UUID
	Nickname string
	PresetID string
	Level    int
	Streak   int
	WeeklyXP int
	Rank     int
	IsMe     bool
}

// toEntry переводит строку репозитория в Entry, вычисляя уровень по той же
// кривой, что и internal/pet — один источник кривой на весь проект.
func toEntry(r Row, curve config.Curve, meID uuid.UUID) (Entry, error) {
	progress, err := config.LevelFor(r.TotalXP, curve)
	if err != nil {
		return Entry{}, fmt.Errorf("social: уровень по опыту %d: %w", r.TotalXP, err)
	}
	return Entry{
		UserID:   r.UserID,
		Nickname: r.Name,
		PresetID: r.PresetID,
		Level:    progress.Level,
		Streak:   0, // стрика ещё нет — честный ноль, не выдумка
		WeeklyXP: r.WeeklyXP,
		Rank:     r.Rank,
		IsMe:     r.UserID == meID,
	}, nil
}

// cursorPrefix — версия формата курсора. Если формат когда-нибудь изменится,
// старые курсоры с клиентов (закладки, кэш) должны отвергаться понятной
// ошибкой, а не разбираться неправильно.
const cursorPrefix = "v1:"

// EncodeCursor кодирует ранг последней строки страницы.
//
// Курсор — это ранг, не смещение (OFFSET): при OFFSET-пагинации вставка новой
// строки между запросами сдвигает все последующие страницы и что-то либо
// повторяется, либо теряется. Ранг — устойчивый ключ: следующая страница —
// это «всё с рангом больше этого», независимо от того, что изменилось выше.
func EncodeCursor(lastRank int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + strconv.Itoa(lastRank)))
}

// DecodeCursor восстанавливает ранг из курсора клиента. 0 (нет курсора) —
// не ошибка, это первая страница.
func DecodeCursor(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrBadCursor, err)
	}
	s := string(decoded)
	if !strings.HasPrefix(s, cursorPrefix) {
		return 0, fmt.Errorf("%w: неизвестная версия курсора", ErrBadCursor)
	}
	rank, err := strconv.Atoi(strings.TrimPrefix(s, cursorPrefix))
	if err != nil || rank < 0 {
		return 0, fmt.Errorf("%w: ранг не число", ErrBadCursor)
	}
	return rank, nil
}

// --- Прикладной слой ---------------------------------------------------

// weeklyWindow — сколько дней назад включительно считается «неделя опыта».
// Контракт: «Ранжирование по XP за 7 дней» — сегодня плюс 6 предыдущих дней.
const weeklyWindowDays = 7

// Service собирает сценарии лидерборда.
type Service struct {
	repo  *Repo
	clk   clock.Clock
	curve config.Curve
}

// NewService собирает сервис. Как и pet.NewService, отказывается собираться
// на бессмысленной кривой: ошибка в константах — неработающий сервис,
// падать на старте, а не на первом запросе.
func NewService(repo *Repo, clk clock.Clock, curve config.Curve) (*Service, error) {
	switch {
	case repo == nil:
		return nil, errors.New("social: нет репозитория")
	case clk == nil:
		return nil, errors.New("social: нет часов")
	}
	if err := curve.Validate(); err != nil {
		return nil, err
	}
	return &Service{repo: repo, clk: clk, curve: curve}, nil
}

// Page — страница лидерборда плюс отдельная строка запрашивающего.
type Page struct {
	Items      []Entry
	Me         *Entry // nil, если у запрашивающего ещё нет питомца — ему нечем ранжироваться
	NextCursor string // пусто, если страница последняя
}

// List возвращает страницу scope=top.
func (s *Service) List(ctx context.Context, meID uuid.UUID, cursor string, limit int) (Page, error) {
	afterRank, err := DecodeCursor(cursor)
	if err != nil {
		return Page{}, err
	}

	since := weekWindowStart(s.clk.Now())

	// limit+1: узнать, есть ли следующая страница, без отдельного COUNT-запроса.
	rows, err := s.repo.Top(ctx, since, afterRank, limit+1)
	if err != nil {
		return Page{}, err
	}

	var nextCursor string
	if len(rows) > limit {
		nextCursor = EncodeCursor(rows[limit-1].Rank)
		rows = rows[:limit]
	}

	items := make([]Entry, 0, len(rows))
	for _, r := range rows {
		e, entryErr := toEntry(r, s.curve, meID)
		if entryErr != nil {
			return Page{}, entryErr
		}
		items = append(items, e)
	}

	page := Page{Items: items, NextCursor: nextCursor}

	meRow, err := s.repo.ByUser(ctx, since, meID)
	switch {
	case errors.Is(err, ErrNoEntry):
		// У запрашивающего нет питомца — не ошибка страницы, просто нечего
		// показать в поле me (контракт помечает Me как необязательное поле).
	case err != nil:
		return Page{}, err
	default:
		meEntry, entryErr := toEntry(meRow, s.curve, meID)
		if entryErr != nil {
			return Page{}, entryErr
		}
		page.Me = &meEntry
	}

	return page, nil
}

// weekWindowStart возвращает первый день окна в UTC.
//
// UTC, а не таймзона аккаунта: лидерборд глобальный, у участников разные
// таймзоны, и общего «начала недели» без единой точки отсчёта не существует.
// pet_action_log.day уже хранится по таймзоне аккаунта КАЖДОГО действия
// (docs/ARCHITECTURE.md), поэтому сравнение здесь по UTC-дате — это
// провизорное упрощение: окно «плавает» на величину смещения таймзоны
// пользователя относительно UTC. Контракт не уточняет часовой пояс окна
// лидерборда явно; более точная версия требует хранить день в UTC отдельно
// от дня в таймзоне аккаунта, что не входит в этот срез.
func weekWindowStart(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -(weeklyWindowDays - 1))
}

// --- Сводка дня ----------------------------------------------------------

// actionLabels — человекочитаемые подписи действий ухода для разбивки дня.
// Контракт не задаёт их формат (просто string), текст на русском — как весь
// остальной пользовательский текст проекта (см. AGENTS.md → Language).
var actionLabels = map[string]string{
	string(pet.ActionFeed):  "Покормить",
	string(pet.ActionPlay):  "Поиграть",
	string(pet.ActionWash):  "Помыть",
	string(pet.ActionSleep): "Уложить спать",
	string(pet.ActionWake):  "Разбудить",
}

// BreakdownEntry — одна строка разбивки дня: сколько раз и на сколько опыта
// принесло одно действие ухода.
type BreakdownEntry struct {
	Key   string
	Label string
	Count int
	XP    int
}

// Summary — сводка суток, как её видит клиент. Урезанная часть контракта
// (docs/openapi.json → DailySummary): стрик-система не построена
// (docs/DECISIONS.md, AGENTS.md → стоп-вопросы), поэтому Multiplier
// честно всегда 1, а Streak/Tomorrow в ответ вообще не попадают —
// handler.go оставляет соответствующие поля nil, а не подсовывает
// выдуманные данные под видом настоящих. AiNote в контракте есть и
// заполняется — см. handler.go: считает internal/advisor поверх Actions
// ниже и internal/rewards.Service.Next, сам Summary остаётся об этом не в
// курсе (в domain-слое сети и внешних сервисов нет).
type Summary struct {
	Date        time.Time
	ShouldShow  bool
	XPTotal     int
	Multiplier  float64
	Breakdown   []BreakdownEntry
	LevelBefore int
	LevelAfter  int
	// HasPetSnapshot — было ли за сутки хоть одно действие ухода: если нет,
	// StatsAfter/MoodAfter/Actions нечем заполнить, контракт помечает pet как
	// необязательное поле именно для этого случая.
	HasPetSnapshot bool
	StatsAfter     pet.Stats
	MoodAfter      pet.Mood
	// Actions — суточные лимиты ухода на момент последнего действия (тот же
	// снимок, что StatsAfter/MoodAfter). Нужен только advisor.Situation в
	// handler.go — сам Summary их не читает и не решает по ним ничего.
	Actions map[pet.ActionKind]pet.Availability
}

// Summary возвращает сводку конкретных суток пользователя. date — уже
// нормализованная календарная дата, полночь UTC (см. handler.go); nil
// значит «сегодня», посчитанное по часам сервиса, — ровно тот же приём,
// что pet.Actor.day с nil-локацией: пока аккаунтов и их таймзон нет, сутки
// везде по UTC.
func (s *Service) Summary(ctx context.Context, userID uuid.UUID, date *time.Time) (Summary, error) {
	day := s.today()
	if date != nil {
		day = *date
	}

	hasPet, err := s.repo.HasPet(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	if !hasPet {
		return Summary{Date: day}, nil
	}

	raw, err := s.repo.Day(ctx, userID, day)
	if err != nil {
		return Summary{}, err
	}

	before, err := config.LevelFor(raw.TotalXPBefore, s.curve)
	if err != nil {
		return Summary{}, fmt.Errorf("social: уровень до суток: %w", err)
	}
	after, err := config.LevelFor(raw.TotalXPAfter, s.curve)
	if err != nil {
		return Summary{}, fmt.Errorf("social: уровень после суток: %w", err)
	}

	out := Summary{
		Date:        day,
		ShouldShow:  raw.ShouldShow,
		XPTotal:     raw.XPTotal,
		Multiplier:  1, // честная единица: стрик-бонуса не существует
		LevelBefore: before.Level,
		LevelAfter:  after.Level,
	}
	out.Breakdown = make([]BreakdownEntry, 0, len(raw.Breakdown))
	for _, b := range raw.Breakdown {
		label, ok := actionLabels[b.Kind]
		if !ok {
			label = b.Kind
		}
		out.Breakdown = append(out.Breakdown, BreakdownEntry{Key: b.Kind, Label: label, Count: b.Count, XP: b.XP})
	}

	// StatsAfter/MoodAfter берутся из результата, который в момент действия
	// уже посчитал internal/pet (та же самая View, что отдаёт /pet/act) — не
	// пересчитываются заново: второй способ посчитать настроение — второе
	// место, которое может разойтись с первым при следующей правке экономики.
	if raw.LastAction != nil {
		out.HasPetSnapshot = true
		out.StatsAfter = raw.LastAction.Pet.Stats
		out.MoodAfter = raw.LastAction.Pet.Mood
		out.Actions = raw.LastAction.Pet.Actions
	}

	return out, nil
}

// MarkSeen отмечает сутки просмотренными. Тонкая обёртка над repo.go:
// добавлена ради того же инварианта, что и у остальных методов сервиса —
// handler.go не имеет права вызывать Repo напрямую (AGENTS.md → Never).
func (s *Service) MarkSeen(ctx context.Context, userID uuid.UUID, date time.Time) error {
	return s.repo.MarkSeen(ctx, userID, date)
}

// today — сегодняшняя календарная дата по часам сервиса, полночь UTC.
func (s *Service) today() time.Time {
	y, m, d := s.clk.Now().UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
