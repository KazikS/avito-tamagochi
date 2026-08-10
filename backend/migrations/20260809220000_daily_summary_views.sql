-- +goose Up

-- daily_summary_views — какие сутки пользователь уже отметил просмотренными
-- (POST /summary/daily/seen). Отдельная таблица, не колонка в pets: сводка
-- существует по одной на пользователя НА КАЖДЫЕ сутки, а не одна на
-- пользователя — прошлые дни тоже можно запросить (GET .../daily?date=).
--
-- Это единственная НОВАЯ таблица в срезе сводки: всё остальное (сколько
-- опыта, что изменилось у питомца) читается из уже существующих pets и
-- pet_action_log — см. internal/social/repo.go.
CREATE TABLE daily_summary_views (
    user_id UUID NOT NULL,
    date    DATE NOT NULL,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, date)
);

-- +goose Down
DROP TABLE daily_summary_views;
