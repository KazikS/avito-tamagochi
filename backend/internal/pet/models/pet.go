// Package models
package models

import (
	"time"

	"github.com/google/uuid"
)

type stage uint8

const (
	Newbie stage = iota
	Seeker
	Expert
	Guardian
)

type stats uint8

const (
	Hunger stats = iota
	Joy
	Cleanliness
	Energy
)

type mood string

const (
	Happy mood = "happy"
	Sad   mood = "sad"
)

type Pet struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Stage     stage     `json:"stage"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PetState struct {
	PetID              uuid.UUID
	Mood               mood
	IsSleeping         bool
	LastDecayAppliedAt time.Time
}
