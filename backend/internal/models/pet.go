// Package models содержит доменные модели и структуры данных для сервиса Питомца (Pet Service).
package models

import (
	"time"

	"github.com/google/uuid"
)

// stage — текущая стадия эволюции питомца (от 1 до 4):
// 1: Новичок (1–4 ур)
// 2: Искатель (5–9 ур)
// 3: Знаток (10–17 ур)
// 4: Хранитель (18–25 ур)
// Влияет на визуальное отображение питомца на фронтенде.
type stage uint8

const (
	Newbie stage = iota
	Seeker
	Expert
	Guardian
)

type stats uint8

// Mood — текущее настроение питомца. Рассчитывается на основе показателей:
// - "happy"    : все показатели >= 60 (множитель XP x1.15)
// - "neutral"  : все показатели >= 40 (множитель XP x1.0)
// - "sad"      : хотя бы один < 40   (множитель XP x0.85)
type mood string

const (
	Happy   mood = "happy"
	neutral mood = "neutral"
	Sad     mood = "sad"
)

// action — тип выполненного действия.
type action string

const (
	Feed  action = "feed"  // покормить (лимит: 2 раза в сутки, базовая награда 10 XP)
	Play  action = "play"  // поиграть  (лимит: 2 раза в сутки, базовая награда 12 XP)
	Wash  action = "wash"  // "wash"  : помыть    (лимит: 1 раз в сутки,  базовая награда 10 XP)
	Sleep action = "sleep" // "sleep" : уложить спать (лимит: 1 раз в сутки, базовая награда 8 XP)
)

// Pet представляет собой основную сущность питомца, привязанную к пользователю.
type Pet struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"` // Пользователь Авито

	// Stage — текущая стадия эволюции питомца (от 1 до 4):
	Stage     stage     `json:"stage"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PetState содержит динамические характеристики питомца, его настроение и флаги состояния.
type PetState struct {
	PetID       uuid.UUID `json:"pet_id"`
	Hunger      int       `json:"hunger"`      // Уровень  голода [0-100]
	Joy         int       `json:"joy"`         // Уровень радости [0-100]
	Cleanliness int       `json:"cleanliness"` // Уровень чистоты [0-100]
	Energy      int       `json:"energy"`      // уровень энергии [0-100]

	// Mood - Настроение питомца. Влияет на множитель XP
	Mood mood `json:"mood"`

	// Время последнего обновления статистик для "ленивого" обновления
	LastDecayAppliedAt time.Time `json:"last_decay_applied_at"`
}

// ActionLog служит для фиксации совершённых действий ухода (Daily Limit & Anti-Abuse log).
type ActionLog struct {
	// ID — уникальный идентификатор лога действия.
	ID uuid.UUID `db:"id" json:"id"`

	// PetID — идентификатор питомца, над которым совершено действие.
	PetID uuid.UUID `db:"pet_id" json:"pet_id"`

	// ActionType — тип выполненного действия
	ActionType action `db:"action_type" json:"action_type"`

	// IdempotencyKey — уникальный UUID, генерируемый на клиенте (фронтендом).
	// Используется для защиты от дублирования мутаций при сбоях сети.
	// Повторный запрос с тем же IdempotencyKey вернет статус 200/409 без повторного начисления XP.
	IdempotencyKey string `db:"idempotency_key" json:"idempotency_key"`

	// XPGranted — фактически начисленное количество XP за это действие.
	// Рассчитывается как (Базовый_XP * Mood_Multiplier * Streak_Multiplier).
	XPGranted int `db:"xp_granted" json:"xp_granted"`

	// CreatedAt — время совершения действия.
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}
