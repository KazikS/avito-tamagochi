// Package postgres открывает пул подключений и накатывает миграции.
//
// Пакет платформенный: он ниже фич и не знает ни про контракт, ни про домен —
// правило depguard `platform-layer` запрещает ему импортировать internal/**.
// Поэтому здесь нет ни одного доменного типа, только соединение и схема.
//
// Миграции живут в отдельном пакете tamagochi/migrations и встроены в бинарь:
// накатывать их из pkg/pgtest, из `make migrate` и из контейнера нужно
// одинаково, а каталог с .sql есть не везде.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"tamagochi/migrations"
)

// connectTimeout ограничивает ожидание первой связи с базой.
//
// Без него старт сервера с недоступной базой висит до таймаута ядра — это
// минуты, в течение которых docker compose up --wait не может понять,
// поднялся сервис или умер.
const connectTimeout = 10 * time.Second

// ErrNoDSN — строка подключения пуста.
var ErrNoDSN = errors.New("postgres: пустая строка подключения")

// New открывает пул и проверяет, что база действительно отвечает.
//
// Ping здесь не формальность: pgxpool соединяется лениво, поэтому без него
// New вернул бы рабочий на вид пул и с недоступной базой, а первая ошибка
// прилетела бы пользователю в запросе вместо того, чтобы уронить старт.
func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if dsn == "" {
		return nil, ErrNoDSN
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: разбор строки подключения: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: создание пула: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if pingErr := pool.Ping(pingCtx); pingErr != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: база не отвечает: %w", pingErr)
	}

	return pool, nil
}

// Migrate накатывает все миграции до последней версии.
//
// goose работает через database/sql, а не через pgxpool, поэтому здесь
// открывается отдельное соединение на время миграции и закрывается сразу
// после. Держать его в пуле незачем: миграции идут один раз на старте.
func Migrate(ctx context.Context, dsn string) error {
	if dsn == "" {
		return ErrNoDSN
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("postgres: разбор строки подключения: %w", err)
	}

	db := stdlib.OpenDB(*cfg.ConnConfig)
	defer func() {
		// Ошибку закрытия обрабатывать некуда: миграции уже накачены или уже
		// провалились, и статус операции она не меняет.
		_ = db.Close()
	}()

	goose.SetBaseFS(migrations.FS)
	// Логгер goose по умолчанию печатает в stdout на каждый прогон, включая
	// каждый тест. Молчим: результат виден по ошибке, а не по логу.
	goose.SetLogger(goose.NopLogger())
	if dialectErr := goose.SetDialect("postgres"); dialectErr != nil {
		return fmt.Errorf("postgres: диалект goose: %w", dialectErr)
	}

	// "." — корень встроенной ФС: пакет migrations встраивает свои .sql
	// без вложенного каталога.
	if upErr := goose.UpContext(ctx, db, "."); upErr != nil {
		return fmt.Errorf("postgres: накат миграций: %w", upErr)
	}
	return nil
}
