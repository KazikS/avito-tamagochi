package rewards

// Слой данных: единственное место с SQL для наград. Критический путь
// (см. AGENTS.md рядом): ошибка здесь — задвоенный грант или награда,
// которой можно поделиться.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrKeyMismatch — повторный claim той же награды другим Idempotency-Key.
// Контракт (POST /rewards/{rewardId}/claim): 409 REWARD_ALREADY_CLAIMED.
var ErrKeyMismatch = errors.New("rewards: другой idempotency-key по уже выданной награде")

// ErrNotClaimed — redeem-click раньше claim: применять нечего.
var ErrNotClaimed = errors.New("rewards: награда ещё не выдана")

// Grant — то, что фактически лежит в reward_grants.
type Grant struct {
	RewardID       string
	IdempotencyKey uuid.UUID
	GrantedAt      time.Time
	UsedAt         *time.Time
}

// Repo — доступ к reward_grants и к total_xp питомца (та же таблица pets,
// что и internal/pet — READ ONLY поверх чужих данных, ровно как это уже
// делает internal/social для лидерборда).
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo собирает репозиторий.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// PetTotalXP — суммарный опыт питомца вне транзакции: для списка наград и
// «следующей» блокировка не нужна, только текущее значение на момент чтения.
func (r *Repo) PetTotalXP(ctx context.Context, userID uuid.UUID) (totalXP int, hasPet bool, err error) {
	err = r.pool.QueryRow(ctx, `SELECT total_xp FROM pets WHERE user_id = $1`, userID).Scan(&totalXP)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("rewards: чтение опыта питомца: %w", err)
	default:
		return totalXP, true, nil
	}
}

// GrantsByUser читает все гранты пользователя — списку наград нужен статус
// каждой позиции каталога, не одной.
func (r *Repo) GrantsByUser(ctx context.Context, userID uuid.UUID) ([]Grant, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT reward_id, idempotency_key, granted_at, used_at FROM reward_grants WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("rewards: чтение грантов: %w", err)
	}
	defer rows.Close()

	var out []Grant
	for rows.Next() {
		var g Grant
		if scanErr := rows.Scan(&g.RewardID, &g.IdempotencyKey, &g.GrantedAt, &g.UsedAt); scanErr != nil {
			return nil, fmt.Errorf("rewards: чтение гранта: %w", scanErr)
		}
		out = append(out, g)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("rewards: обход грантов: %w", rowsErr)
	}
	return out, nil
}

// Claim — ЕДИНСТВЕННОЕ место, которое пишет в reward_grants.
//
// Порядок операций — дословно AGENTS.md рядом → «Всегда»: BEGIN →
// SELECT ... FOR UPDATE по пользователю → проверка eligibility →
// INSERT INTO reward_grants → COMMIT.
//
// FOR UPDATE берётся на pets, не на новую таблицу users (которой в проекте
// ещё нет — feat/auth не смержена): это тот же ряд, от которого зависит
// eligible, и блокировки на нём достаточно, чтобы сериализовать конкурентные
// claim'ы одного пользователя без отдельного замка. Пользователю без
// питомца блокировать нечего (SELECT ... FOR UPDATE не находит строк), но
// это безопасно: eligible ниже получает hasPet=false и всегда отказывает
// раньше, чем дело дойдёт до INSERT.
//
// eligible получает totalXP и hasPet, прочитанные ВНУТРИ этой же
// транзакции — не устаревший снимок снаружи. Тот же приём, что
// pet.Repo.ApplyAction: доменное решение принимается над свежими,
// заблокированными данными, а не до начала транзакции.
func (r *Repo) Claim(
	ctx context.Context,
	userID uuid.UUID,
	rewardID string,
	idempotencyKey uuid.UUID,
	eligible func(totalXP int, hasPet bool) error,
) (grant Grant, totalXP int, replayed bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Grant{}, 0, false, fmt.Errorf("rewards: старт транзакции: %w", err)
	}
	// Ошибку Rollback после успешного Commit обрабатывать некуда: pgx вернёт
	// ErrTxClosed, это не сигнал о проблеме, а не выполнить его после раннего
	// return — течь соединения.
	defer func() { _ = tx.Rollback(ctx) }()

	var hasPet bool
	lockErr := tx.QueryRow(ctx, `SELECT total_xp FROM pets WHERE user_id = $1 FOR UPDATE`, userID).Scan(&totalXP)
	switch {
	case errors.Is(lockErr, pgx.ErrNoRows):
		hasPet = false
	case lockErr != nil:
		return Grant{}, 0, false, fmt.Errorf("rewards: блокировка питомца: %w", lockErr)
	default:
		hasPet = true
	}

	// Уже выдано? Читаем СНАЧАЛА, до eligibility: легитимный повтор обязан
	// вернуть прежний результат, даже если условие с тех пор перестало
	// формально выполняться (тот же принцип, что повтор действия ухода в
	// internal/pet возвращает прежний result, а не пересчитывает заново).
	existing := Grant{RewardID: rewardID}
	selErr := tx.QueryRow(ctx,
		`SELECT idempotency_key, granted_at, used_at FROM reward_grants WHERE user_id = $1 AND reward_id = $2`,
		userID, rewardID,
	).Scan(&existing.IdempotencyKey, &existing.GrantedAt, &existing.UsedAt)
	switch {
	case selErr == nil:
		if existing.IdempotencyKey != idempotencyKey {
			return Grant{}, totalXP, false, ErrKeyMismatch
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return Grant{}, totalXP, false, fmt.Errorf("rewards: подтверждение повтора: %w", commitErr)
		}
		return existing, totalXP, true, nil
	case errors.Is(selErr, pgx.ErrNoRows):
		// Гранта ещё нет — обычный путь ниже.
	default:
		return Grant{}, totalXP, false, fmt.Errorf("rewards: чтение гранта: %w", selErr)
	}

	if eligErr := eligible(totalXP, hasPet); eligErr != nil {
		return Grant{}, totalXP, false, eligErr
	}

	grant = Grant{RewardID: rewardID, IdempotencyKey: idempotencyKey}
	insErr := tx.QueryRow(ctx,
		`INSERT INTO reward_grants (user_id, reward_id, idempotency_key)
		 VALUES ($1, $2, $3)
		 RETURNING granted_at`,
		userID, rewardID, idempotencyKey,
	).Scan(&grant.GrantedAt)
	if insErr != nil {
		return Grant{}, totalXP, false, fmt.Errorf("rewards: запись гранта: %w", insErr)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return Grant{}, totalXP, false, fmt.Errorf("rewards: подтверждение гранта: %w", commitErr)
	}
	return grant, totalXP, false, nil
}

// MarkUsed переводит грант в used — POST /rewards/{rewardId}/redeem-click.
// Идемпотентна: повторный клик по уже применённой награде не ошибка (тот же
// приём, что MarkSeen в internal/social) — ошибка только там, где применять
// нечего вообще.
func (r *Repo) MarkUsed(ctx context.Context, userID uuid.UUID, rewardID string) error {
	var grantedAt time.Time
	updErr := r.pool.QueryRow(ctx, `
		UPDATE reward_grants SET used_at = now()
		WHERE user_id = $1 AND reward_id = $2 AND used_at IS NULL
		RETURNING granted_at`,
		userID, rewardID,
	).Scan(&grantedAt)
	if updErr == nil {
		return nil
	}
	if !errors.Is(updErr, pgx.ErrNoRows) {
		return fmt.Errorf("rewards: применение награды: %w", updErr)
	}

	// UPDATE не задел ни одной строки: либо гранта нет вовсе, либо он уже
	// применён — различаем отдельным чтением, потому что реакция разная.
	var used bool
	readErr := r.pool.QueryRow(ctx,
		`SELECT used_at IS NOT NULL FROM reward_grants WHERE user_id = $1 AND reward_id = $2`,
		userID, rewardID,
	).Scan(&used)
	switch {
	case errors.Is(readErr, pgx.ErrNoRows):
		return ErrNotClaimed
	case readErr != nil:
		return fmt.Errorf("rewards: проверка гранта: %w", readErr)
	default:
		// used обязано быть true: между UPDATE и этим SELECT грант не мог ни
		// исчезнуть, ни снова стать used_at IS NULL.
		return nil
	}
}
