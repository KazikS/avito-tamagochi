package repository

import "github.com/jackc/pgx/v5/pgxpool"

type PetRepository struct{
	pool *pgxpool.Pool
}

