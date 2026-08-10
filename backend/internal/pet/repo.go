package pet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Слой данных: единственное место с SQL. Доменных решений здесь нет — что
// именно записать, решает функция decide, которую передаёт service.go.
// Разделение проверяется depguard'ом: service.go не может импортировать pgx,
// а этот файл не принимает решений, он их только последовательно применяет.

// Ошибки слоя данных.
var (
	// ErrNoPet — у пользователя ещё нет питомца.
	ErrNoPet = errors.New("pet: питомца нет")
	// ErrPetExists — питомец уже создан.
	ErrPetExists = errors.New("pet: питомец уже создан")
)

// Record — питомец, как он лежит в базе.
//
// Stats и StatsAt — снимок и его момент, а не текущее состояние: текущее
// считается функцией Decay при чтении. См. преамбулу пакета.
type Record struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	PresetID string
	Name     string
	Stats    Stats
	Sleeping bool
	StatsAt  time.Time
	TotalXP  int
}

// Snapshot — всё, что домен видит перед решением о действии.
type Snapshot struct {
	// Pet — питомец на момент начала транзакции.
	Pet Record
	// SpentToday — сколько опыта ухода уже начислено за текущие сутки.
	SpentToday int
	// CountsToday — сколько раз каждое действие уже сделано за текущие сутки.
	// Нужен для суточного лимита самого действия (Action.DailyLimit), который
	// не то же самое, что суточный кап опыта.
	CountsToday map[ActionKind]int
}

// Decision — что домен решил записать.
type Decision struct {
	// Stats и Sleeping — новое состояние питомца.
	Stats    Stats
	Sleeping bool
	// StatsAt — момент, на который посчитано состояние.
	StatsAt time.Time
	// XPGranted — сколько опыта начислить; уходит и в total_xp, и в журнал.
	XPGranted int
	// Result — ответ клиенту целиком, сериализованный.
	//
	// Хранится, потому что контракт требует от повтора ПРЕЖНИЙ результат:
	// пересчёт на новое время показал бы при двойном тапе два разных
	// состояния питомца.
	Result json.RawMessage
}

// Repo — доступ к питомцам в Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo собирает репозиторий.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// petColumns перечисляет колонки в порядке, в котором их читает scanPet.
// Одна строка вместо трёх копий списка: разъехавшийся порядок колонок и
// полей — ошибка, которую компилятор не видит.
const petColumns = `id, user_id, preset_id, name, hunger, joy, clean, energy, sleeping, stats_at, total_xp`

// scanPet читает Record из строки результата.
func scanPet(row pgx.Row) (Record, error) {
	var r Record
	err := row.Scan(
		&r.ID, &r.UserID, &r.PresetID, &r.Name,
		&r.Stats.Hunger, &r.Stats.Joy, &r.Stats.Clean, &r.Stats.Energy,
		&r.Sleeping, &r.StatsAt, &r.TotalXP,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNoPet
	}
	if err != nil {
		return Record{}, fmt.Errorf("pet: чтение питомца: %w", err)
	}
	return r, nil
}

// Create заводит питомца. Второй питомец тому же пользователю — ErrPetExists.
//
// Однократность держит UNIQUE(user_id) в схеме, а не проверка «а есть ли уже»:
// между такой проверкой и вставкой помещается второй запрос, и оба увидели бы
// «питомца нет».
func (r *Repo) Create(ctx context.Context, rec Record) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO pets (id, user_id, preset_id, name, hunger, joy, clean, energy, sleeping, stats_at, total_xp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (user_id) DO NOTHING`,
		rec.ID, rec.UserID, rec.PresetID, rec.Name,
		rec.Stats.Hunger, rec.Stats.Joy, rec.Stats.Clean, rec.Stats.Energy,
		rec.Sleeping, rec.StatsAt, rec.TotalXP,
	)
	if err != nil {
		return fmt.Errorf("pet: создание питомца: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPetExists
	}
	return nil
}

// ByUser возвращает питомца пользователя.
func (r *Repo) ByUser(ctx context.Context, userID uuid.UUID) (Record, error) {
	return scanPet(r.pool.QueryRow(ctx, `SELECT `+petColumns+` FROM pets WHERE user_id = $1`, userID))
}

// SpentToday возвращает опыт ухода, начисленный за указанные сутки.
func (r *Repo) SpentToday(ctx context.Context, userID uuid.UUID, day time.Time) (int, error) {
	var spent int
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(xp_granted), 0) FROM pet_action_log WHERE user_id = $1 AND day = $2`,
		userID, day,
	).Scan(&spent)
	if err != nil {
		return 0, fmt.Errorf("pet: расход опыта за сутки: %w", err)
	}
	return spent, nil
}

// countsToday читает, сколько раз каждое действие сделано за сутки.
//
// Принимает интерфейс запроса, а не пул: внутри транзакции считать надо тем же
// tx, иначе счётчик придёт из другого снимка, чем блокировка питомца.
func countsToday(ctx context.Context, q pgxQuerier, userID uuid.UUID, day time.Time) (map[ActionKind]int, error) {
	rows, err := q.Query(ctx,
		`SELECT kind, COUNT(*) FROM pet_action_log WHERE user_id = $1 AND day = $2 GROUP BY kind`,
		userID, day,
	)
	if err != nil {
		return nil, fmt.Errorf("pet: счётчики действий за сутки: %w", err)
	}
	defer rows.Close()

	counts := make(map[ActionKind]int, len(ActionKinds))
	for rows.Next() {
		var kind string
		var n int
		if scanErr := rows.Scan(&kind, &n); scanErr != nil {
			return nil, fmt.Errorf("pet: чтение счётчика действий: %w", scanErr)
		}
		counts[ActionKind(kind)] = n
	}
	// rows.Err() обязателен: ошибка, случившаяся посреди выборки, иначе
	// выглядит как пустой результат. Это и проверяет линтер rowserrcheck.
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("pet: обход счётчиков действий: %w", rowsErr)
	}
	return counts, nil
}

// pgxQuerier — общее у пула и транзакции. Объявлен здесь, а не взят из pgx,
// потому что в pgx такого интерфейса нет, а countsToday обязана работать и
// внутри транзакции, и вне её.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// CountsToday читает счётчики действий за сутки вне транзакции.
func (r *Repo) CountsToday(ctx context.Context, userID uuid.UUID, day time.Time) (map[ActionKind]int, error) {
	return countsToday(ctx, r.pool, userID, day)
}

// ApplyAction выполняет действие ухода одной транзакцией.
//
// Порядок шагов — не стиль, а то, чем держатся инварианты 1 и 4:
//
//  1. Строка питомца берётся FOR UPDATE. Это сериализует одновременные
//     действия одного пользователя: второй запрос ждёт коммита первого,
//     а не считает по устаревшему снимку.
//  2. Журнал проверяется ПОСЛЕ взятия блокировки. В READ COMMITTED каждый
//     запрос видит свежий снимок, поэтому второй участник гонки, дождавшись
//     блокировки, увидит уже закоммиченную запись первого и вернёт её ответ.
//     Проверка до блокировки этого не даёт: оба увидели бы пустой журнал.
//  3. Запись в журнал и обновление питомца — в одной транзакции с чтением.
//     Опыт без записи в журнал невозможен по построению: они коммитятся
//     вместе или не коммитятся вовсе.
//
// replayed=true означает, что действие с таким actionID уже выполнялось и
// возвращён прежний результат; питомец при этом не менялся.
func (r *Repo) ApplyAction(
	ctx context.Context,
	userID, actionID uuid.UUID,
	kind ActionKind,
	day time.Time,
	decide func(Snapshot) (Decision, error),
) (result json.RawMessage, replayed bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("pet: начало транзакции: %w", err)
	}
	defer func() {
		// Rollback после успешного Commit возвращает ErrTxClosed — это не
		// ошибка, а нормальный путь, поэтому она гасится здесь и только она.
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("pet: откат транзакции: %w", rbErr))
		}
	}()

	rec, err := scanPet(tx.QueryRow(ctx, `SELECT `+petColumns+` FROM pets WHERE user_id = $1 FOR UPDATE`, userID))
	if err != nil {
		return nil, false, err
	}

	var prior json.RawMessage
	scanErr := tx.QueryRow(ctx,
		`SELECT result FROM pet_action_log WHERE user_id = $1 AND action_id = $2`,
		userID, actionID,
	).Scan(&prior)
	switch {
	case scanErr == nil:
		return prior, true, nil
	case !errors.Is(scanErr, pgx.ErrNoRows):
		return nil, false, fmt.Errorf("pet: чтение журнала действий: %w", scanErr)
	}

	var spent int
	if err = tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(xp_granted), 0) FROM pet_action_log WHERE user_id = $1 AND day = $2`,
		userID, day,
	).Scan(&spent); err != nil {
		return nil, false, fmt.Errorf("pet: расход опыта за сутки: %w", err)
	}

	counts, err := countsToday(ctx, tx, userID, day)
	if err != nil {
		return nil, false, err
	}

	decision, err := decide(Snapshot{Pet: rec, SpentToday: spent, CountsToday: counts})
	if err != nil {
		return nil, false, err
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO pet_action_log (user_id, action_id, kind, xp_granted, day, result)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, actionID, string(kind), decision.XPGranted, day, []byte(decision.Result),
	); err != nil {
		return nil, false, fmt.Errorf("pet: запись в журнал действий: %w", err)
	}

	if _, err = tx.Exec(ctx, `
		UPDATE pets
		   SET hunger = $2, joy = $3, clean = $4, energy = $5,
		       sleeping = $6, stats_at = $7, total_xp = total_xp + $8
		 WHERE user_id = $1`,
		userID,
		decision.Stats.Hunger, decision.Stats.Joy, decision.Stats.Clean, decision.Stats.Energy,
		decision.Sleeping, decision.StatsAt, decision.XPGranted,
	); err != nil {
		return nil, false, fmt.Errorf("pet: обновление питомца: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("pet: коммит транзакции: %w", err)
	}
	return decision.Result, false, nil
}
