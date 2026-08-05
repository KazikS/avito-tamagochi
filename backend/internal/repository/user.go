package repository

import (
	"context"
	"errors"
	"fmt"
	"tamagochi/backend/internal/pet/models"
	"tamagochi/backend/pkg/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UserRepository struct {
	pg *pgxpool.Pool
}

func NewUserRepository(ctx context.Context, cfg postgres.Config) (*UserRepository, error) {
	pool, err := postgres.NewPostgresPool(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("NewUserRepository: failed to connect to users postgres: %w", err)
	}
	return &UserRepository{pg: pool.Pool}, nil
}

func (ur *UserRepository) CreateUser(ctx context.Context, user *models.User) (int, error) {
	var id int
	queryRow := ur.pg.QueryRow(ctx, `INSERT INTO users (login, password) VALUES ($1, $2) RETURNING id`, user.Login, user.Password)
	err := queryRow.Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("CreateUser: failed to query users: %w", err)
	}
	return id, nil
}

func (ur *UserRepository) GetByID(ctx context.Context, id int) (*models.User, error) {
	user := &models.User{}
	queryRow := ur.pg.QueryRow(ctx, `SELECT id, login, password FROM users WHERE id = $1;`, id)
	err := queryRow.Scan(&user.ID, &user.Login, &user.Password)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "User not found")
	} else if err != nil {
		return nil, fmt.Errorf("GetByID: failed to query users by id: %w", err)
	}
	return user, nil
}
