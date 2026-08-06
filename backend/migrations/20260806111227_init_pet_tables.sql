-- +goose Up
-- 1. Таблица пользователей (Профиль и общая прогрессия)
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    
    -- Прогрессия вынесена сюда для быстрого доступа (Leaderboard будет строиться по xp)
    xp INT NOT NULL DEFAULT 0,
    level INT NOT NULL DEFAULT 1,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Таблица питомца (Объединяет Pet и PetState для скорости)
CREATE TABLE pets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    
    stage INT NOT NULL DEFAULT 1, -- 1: Новичок, 2: Искатель и т.д.
    
    -- Характеристики (0-100)
    hunger INT NOT NULL DEFAULT 100,
    joy INT NOT NULL DEFAULT 100,
    cleanliness INT NOT NULL DEFAULT 100,
    energy INT NOT NULL DEFAULT 100,
    
    is_sleeping BOOLEAN NOT NULL DEFAULT false,
    
    -- Время последнего пересчета (lazy decay)
    last_decay_applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Таблица стриков (Прогрессия заходов в игру)
-- Вынесена отдельно, так как обновляется по своей логике
CREATE TABLE streaks (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    
    current_streak INT NOT NULL DEFAULT 0,
    max_streak INT NOT NULL DEFAULT 0,
    total_days_counted INT NOT NULL DEFAULT 0,
    
    freezes_available INT NOT NULL DEFAULT 0,
    
    -- DATE удобно использовать для проверки "засчитан ли день сегодня"
    last_activity_date DATE 
);

-- 4. Лог действий (Защита от абуза и трекинг лимитов)
CREATE TABLE action_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pet_id UUID NOT NULL REFERENCES pets(id) ON DELETE CASCADE,
    
    action_type VARCHAR(20) NOT NULL, -- 'feed', 'play', 'wash', 'sleep'
    xp_granted INT NOT NULL DEFAULT 0,
    
    -- Ключ идемпотентности с уникальным индексом на уровне БД!
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индексы для оптимизации запросов (важно для проверок лимитов)
-- Быстрый поиск действий конкретного питомца за текущий день
CREATE INDEX idx_action_logs_pet_date ON action_logs(pet_id, created_at);
-- Быстрый поиск топа пользователей для Leaderboard
CREATE INDEX idx_users_xp ON users(xp DESC);

-- +goose Down
SELECT 'down SQL query';
