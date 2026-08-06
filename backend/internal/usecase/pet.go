package usecase

import (
	"context"
	"tamagochi/backend/internal/models"
	"tamagochi/backend/internal/repository"

	"github.com/google/uuid"
)


type PetService struct {
	repo repository.PetRepository
}

func (ps *PetService) GetPetState(ctx context.Context, userID uuid.UUID) (*models.PetState, error)