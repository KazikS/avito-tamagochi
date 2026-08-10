package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tamagochi/internal/pet"
)

// Слой данных: единственное место с SQL. Никаких доменных решений — те живут
// в service.go, этот файл только читает готовые числа.

// ErrNoEntry — у пользователя нет питомца, значит нечем ранжироваться.
var ErrNoEntry = errors.New("social: питомца нет — не в рейтинге")

// Repo — чтение лидерборда из тех же таблиц, что использует internal/pet:
// pets (total_xp, preset_id, name) и pet_action_log (xp_granted, day) для
// окна за 7 дней. Отдельных таблиц под лидерборд не заводится — READ ONLY
// поверх чужих данных, ровно так, как это описывает docs/DECISIONS.md
// («Postgres тянет лидерборд... на масштабе хакатона», решение против Redis).
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo собирает репозиторий.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// rankedCTE — общее ранжирование для Top и ByUser: ROW_NUMBER считает
// глобальную позицию по опыту за окно, тай-брейк user_id (см. service.go —
// стрика ещё нет). Один и тот же СTE в обоих запросах: два места, считающих
// ранг по-разному, разошлись бы молча при первой правке одного из них.
const rankedCTE = `
WITH weekly AS (
	SELECT user_id, COALESCE(SUM(xp_granted), 0) AS weekly_xp
	FROM pet_action_log
	WHERE day >= $1
	GROUP BY user_id
),
ranked AS (
	SELECT
		p.user_id,
		p.preset_id,
		p.name,
		p.total_xp,
		COALESCE(w.weekly_xp, 0) AS weekly_xp,
		ROW_NUMBER() OVER (ORDER BY COALESCE(w.weekly_xp, 0) DESC, p.user_id ASC) AS rank
	FROM pets p
	LEFT JOIN weekly w ON w.user_id = p.user_id
)
`

func scanRow(row pgx.Row) (Row, error) {
	var r Row
	var rank int64
	err := row.Scan(&r.UserID, &r.PresetID, &r.Name, &r.TotalXP, &r.WeeklyXP, &rank)
	if errors.Is(err, pgx.ErrNoRows) {
		return Row{}, ErrNoEntry
	}
	if err != nil {
		return Row{}, fmt.Errorf("social: чтение строки рейтинга: %w", err)
	}
	r.Rank = int(rank)
	return r, nil
}

// Top возвращает страницу рейтинга: ранги строго больше afterRank (0 — с
// начала), не больше limit строк.
func (r *Repo) Top(ctx context.Context, since time.Time, afterRank, limit int) ([]Row, error) {
	rows, err := r.pool.Query(ctx, rankedCTE+`
		SELECT user_id, preset_id, name, total_xp, weekly_xp, rank
		FROM ranked
		WHERE rank > $2
		ORDER BY rank
		LIMIT $3`,
		since, afterRank, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("social: страница рейтинга: %w", err)
	}
	defer rows.Close()

	// scanRow принимает pgx.Row, а rows — pgx.Rows: оба сводятся к одному и
	// тому же Scan(dest ...any) error, поэтому rows подходит без адаптера.
	// Так и та, и другая выборка проверяют колонки одним и тем же кодом —
	// разъехавшийся порядок колонок между Top и ByUser был бы ошибкой,
	// которую находят в проде, а не при чтении диффа.
	out := make([]Row, 0, limit)
	for rows.Next() {
		row, scanErr := scanRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("social: обход рейтинга: %w", rowsErr)
	}
	return out, nil
}

// ByUser возвращает ранг конкретного пользователя вне зависимости от
// страницы — контракт требует «me» отдельно от items, независимо от того,
// куда пролистал клиент.
func (r *Repo) ByUser(ctx context.Context, since time.Time, userID uuid.UUID) (Row, error) {
	return scanRow(r.pool.QueryRow(ctx, rankedCTE+`
		SELECT user_id, preset_id, name, total_xp, weekly_xp, rank
		FROM ranked
		WHERE user_id = $2`,
		since, userID,
	))
}

// --- Сводка дня --------------------------------------------------------

// BreakdownRow — сколько раз и на сколько опыта сделано одно действие
// за сутки.
type BreakdownRow struct {
	Kind  string
	Count int
	XP    int
}

// DaySummary — сырые данные для одной сводки: то, что можно вычислить
// из pet_action_log и pets, без домена summary (тот собирает service.go).
type DaySummary struct {
	ShouldShow    bool
	XPTotal       int
	Breakdown     []BreakdownRow
	TotalXPBefore int // сумма опыта СТРОГО до этих суток — для levelBefore
	TotalXPAfter  int // сумма опыта включительно по эти сутки — для levelAfter
	// LastAction — результат последнего действия ухода за эти сутки, откуда
	// берутся statsAfter/moodAfter. nil, если в этот день не было действий.
	LastAction *pet.ActResult
}

// Day возвращает данные сводки за конкретные сутки конкретного пользователя.
//
// day — уже посчитанная календарная дата (UTC, как и остальной пакет — см.
// weekWindowStart), не time.Time с произвольным временем суток: сравнение
// в SQL идёт с типом DATE.
func (r *Repo) Day(ctx context.Context, userID uuid.UUID, day time.Time) (DaySummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT kind, COUNT(*), COALESCE(SUM(xp_granted), 0)
		FROM pet_action_log
		WHERE user_id = $1 AND day = $2
		GROUP BY kind`,
		userID, day,
	)
	if err != nil {
		return DaySummary{}, fmt.Errorf("social: разбивка дня: %w", err)
	}
	var out DaySummary
	for rows.Next() {
		var br BreakdownRow
		if scanErr := rows.Scan(&br.Kind, &br.Count, &br.XP); scanErr != nil {
			rows.Close()
			return DaySummary{}, fmt.Errorf("social: чтение разбивки дня: %w", scanErr)
		}
		out.Breakdown = append(out.Breakdown, br)
		out.XPTotal += br.XP
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		return DaySummary{}, fmt.Errorf("social: обход разбивки дня: %w", rowsErr)
	}
	rows.Close()
	out.ShouldShow = len(out.Breakdown) > 0

	if beforeErr := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(xp_granted), 0) FROM pet_action_log WHERE user_id = $1 AND day < $2`,
		userID, day,
	).Scan(&out.TotalXPBefore); beforeErr != nil {
		return DaySummary{}, fmt.Errorf("social: опыт до суток: %w", beforeErr)
	}
	out.TotalXPAfter = out.TotalXPBefore + out.XPTotal

	if out.ShouldShow {
		var raw []byte
		lastErr := r.pool.QueryRow(ctx, `
			SELECT result FROM pet_action_log
			WHERE user_id = $1 AND day = $2
			ORDER BY created_at DESC
			LIMIT 1`,
			userID, day,
		).Scan(&raw)
		if lastErr != nil {
			return DaySummary{}, fmt.Errorf("social: результат последнего действия: %w", lastErr)
		}
		// pet.ActResult, не самодельная структура: pet_action_log.result —
		// это json.Marshal(pet.ActResult{...}) из internal/pet/service.go,
		// и pet.Stats внутри него БЕЗ json-тегов (сериализуется полями Go —
		// Hunger, не hunger). Раскодировать в тот же тип, которым это было
		// закодировано, — единственный способ не гадать регистр ключей,
		// который здесь уже один раз молча разошёлся (см. internal/pet/ws.go
		// → StatsPayload и её докстринг).
		var last pet.ActResult
		if unmarshalErr := json.Unmarshal(raw, &last); unmarshalErr != nil {
			return DaySummary{}, fmt.Errorf("social: разбор результата последнего действия: %w", unmarshalErr)
		}
		out.LastAction = &last
	}

	return out, nil
}

// HasPet сообщает, есть ли у пользователя питомец вообще — сводке о нём
// нечего показать, если его никогда не было.
func (r *Repo) HasPet(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pets WHERE user_id = $1)`, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("social: проверка питомца: %w", err)
	}
	return exists, nil
}

// MarkSeen отмечает сутки просмотренными. Идемпотентна: повторная отметка
// того же дня — не ошибка (контракт отвечает 204 в обоих случаях).
func (r *Repo) MarkSeen(ctx context.Context, userID uuid.UUID, date time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO daily_summary_views (user_id, date)
		VALUES ($1, $2)
		ON CONFLICT (user_id, date) DO NOTHING`,
		userID, date,
	)
	if err != nil {
		return fmt.Errorf("social: отметка сводки просмотренной: %w", err)
	}
	return nil
}
