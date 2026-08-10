// Package pgtest выдаёт тесту личную схему в настоящем Postgres.
//
// Зачем настоящая база, а не фейк репозитория. Инварианты 1 и 4 из
// docs/ARCHITECTURE.md — однократность начисления при повторе и при гонке —
// держатся не кодом, а ограничениями схемы: PRIMARY KEY (user_id, action_id)
// и поведением ON CONFLICT в транзакции. Фейк, написанный на map с мьютексом,
// проверял бы мой же мьютекс и был бы зелёным при полностью сломанном SQL.
// Такой тест — ровно тот «гейт, который не может покраснеть», из-за которого
// в этом проекте появился mutation-check.
//
// Зачем схема на тест, а не общая база. Тесты идут параллельно и с -race;
// общая база означала бы, что счётчик суточного капа из одного теста портит
// ожидания другого, и падения были бы плавающими. Схема создаётся, миграции
// накатываются в неё, после теста схема удаляется целиком.
//
// Без TEST_DATABASE_URL тесты пропускаются, а не падают: у разработчика без
// поднятого Postgres прогон должен оставаться зелёным. В CI переменная
// выставлена (.github/workflows/ci.yml), поэтому там они выполняются
// по-настоящему — пропуск, который никогда не отменяется, тоже не гейт.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tamagochi/pkg/postgres"
)

// EnvVar — переменная окружения со строкой подключения к тестовой базе.
const EnvVar = "TEST_DATABASE_URL"

// setupTimeout ограничивает создание схемы и накат миграций.
const setupTimeout = 30 * time.Second

// Pool возвращает пул, работающий в личной схеме этого теста.
//
// Если TEST_DATABASE_URL не выставлена — тест пропускается. Схема и всё её
// содержимое удаляются через t.Cleanup, в том числе когда тест упал.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv(EnvVar)
	if dsn == "" {
		t.Skipf("%s не выставлена — интеграционный тест пропущен", EnvVar)
	}

	schema := uniqueSchemaName(t)

	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	// Схему создаём отдельным коротким подключением к базе как есть: пул для
	// теста открывается уже с search_path на эту схему, а установить его до
	// того, как схема существует, нельзя.
	admin, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgtest: не могу подключиться к %s: %v", EnvVar, err)
	}
	if _, createErr := admin.Exec(ctx, "CREATE SCHEMA "+quoteIdent(schema)); createErr != nil {
		admin.Close()
		t.Fatalf("pgtest: создание схемы %s: %v", schema, createErr)
	}
	admin.Close()

	scoped, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatalf("pgtest: не могу построить строку подключения: %v", err)
	}

	// Миграции накатываются в личную схему: search_path уже указывает на неё,
	// поэтому и таблицы, и служебная таблица версий goose создаются внутри.
	if migrateErr := postgres.Migrate(ctx, scoped); migrateErr != nil {
		dropSchema(t, dsn, schema)
		t.Fatalf("pgtest: накат миграций в %s: %v", schema, migrateErr)
	}

	pool, err := postgres.New(ctx, scoped)
	if err != nil {
		dropSchema(t, dsn, schema)
		t.Fatalf("pgtest: пул для схемы %s: %v", schema, err)
	}

	t.Cleanup(func() {
		pool.Close()
		dropSchema(t, dsn, schema)
	})

	return pool
}

// Enabled сообщает, выполняются ли интеграционные тесты в этом окружении.
//
// Нужна там, где пропуск решается до захода в подтест — например, чтобы не
// печатать «SKIP» отдельной строкой на каждый случай таблицы.
func Enabled() bool { return os.Getenv(EnvVar) != "" }

// uniqueSchemaName собирает имя схемы из имени теста и случайного хвоста.
//
// Имя теста — чтобы забытую схему было видно, чей это тест. Случайный хвост —
// потому что имя теста не уникально: подтесты с одинаковым именем в разных
// пакетах и повторные прогоны с -count дали бы столкновение.
func uniqueSchemaName(t *testing.T) string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("pgtest: не могу получить случайные байты: %v", err)
	}

	safe := make([]rune, 0, 24)
	for _, r := range t.Name() {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			safe = append(safe, r)
		case r >= 'A' && r <= 'Z':
			safe = append(safe, r+('a'-'A'))
		default:
			safe = append(safe, '_')
		}
		if len(safe) == 24 {
			break
		}
	}

	return fmt.Sprintf("t_%s_%s", string(safe), hex.EncodeToString(buf[:]))
}

// withSearchPath возвращает ту же строку подключения, но с search_path,
// указывающим на schema.
//
// Через параметр options, а не отдельным SET после подключения: пул открывает
// несколько соединений и переоткрывает их при обрывах, и SET на одном из них
// не распространяется на остальные — часть запросов уходила бы в public.
func withSearchPath(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("pgtest: разбор %s: %w", EnvVar, err)
	}
	q := u.Query()
	q.Set("options", "-c search_path="+schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// dropSchema удаляет схему вместе со всем содержимым.
func dropSchema(t *testing.T, dsn, schema string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	pool, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Logf("pgtest: схема %s осталась в базе, подключиться для удаления не вышло: %v", schema, err)
		return
	}
	defer pool.Close()

	if _, dropErr := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+quoteIdent(schema)+" CASCADE"); dropErr != nil {
		t.Logf("pgtest: схема %s осталась в базе: %v", schema, dropErr)
	}
}

// quoteIdent заключает идентификатор в кавычки для подстановки в SQL.
//
// Имена схем сюда приходят из uniqueSchemaName, то есть уже состоят только из
// букв, цифр и подчёркиваний. Кавычки всё равно ставятся: идентификатор
// нельзя передать плейсхолдером, а «здесь подстановка безопасна, потому что
// выше по коду так сложилось» — это утверждение, которое переживёт ровно до
// первой правки uniqueSchemaName.
func quoteIdent(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '"')
	for _, r := range s {
		if r == '"' {
			out = append(out, '"')
		}
		out = append(out, r)
	}
	out = append(out, '"')
	return string(out)
}
