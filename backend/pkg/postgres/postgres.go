// Package postgres содержит базовое поключение к PostgreSQL
package postgres

import (
	"context"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config описывает переменные и данные, необходимые для работы базой
type Config struct {
	Host     string `env:"HOST"`
	Port     string `env:"PORT"`
	DB       string `env:"DB"`
	Username string `env:"USER"`
	Password string `env:"PASSWORD"`
	MaxConns string `env:"MAX_CONNS"`
	MinConns string `env:"MIN_CONNS"`
}

type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgresPool создает пул подлючений в бд
func NewPostgresPool(ctx context.Context, cfg Config) (*Postgres, error) {
	// urlExample := "postgres://username:password@localhost:5432/database_name?sslmode=disable&pool_min_conns=%d&pool_max_conns=%d"
	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	connstring := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable&pool_min_conns=%s&pool_max_conns=%s",
		cfg.Username,
		cfg.Password,
		addr,
		cfg.DB,
		cfg.MinConns,
		cfg.MaxConns,
	)

	pgPool, err := pgxpool.New(ctx, connstring)
	if err != nil {
		return nil, fmt.Errorf("new: failed to create pool: %w", err)
	}

	return &Postgres{Pool: pgPool}, nil
}	

func (p *Postgres) Check(ctx context.Context) error {
	return p.Pool.Ping(ctx)
}
