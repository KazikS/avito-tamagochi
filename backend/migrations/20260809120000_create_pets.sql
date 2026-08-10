-- +goose Up

-- pets — питомец пользователя.
--
-- Показатели хранятся СНИМКОМ (hunger/joy/clean/energy) плюс момент этого
-- снимка (stats_at). Текущее значение вычисляется при чтении функцией
-- pet.Decay и НЕ записывается обратно: показатели целые, при каждой записи
-- результат округляется вниз, и питомец «худел» бы тем быстрее, чем чаще
-- пользователь открывает экран. См. преамбулу пакета internal/pet.
--
-- Внешнего ключа на users здесь нет намеренно. Схему users создают две ветки
-- по-разному (SERIAL+login против UUID+username), и свести их — разговор с
-- авторами веток, а не решение этой миграции (docs/RECONCILIATION.md).
-- Ключ появится вместе с авторизацией, когда схема users будет одна.
CREATE TABLE pets (
    id         UUID        PRIMARY KEY,
    -- UNIQUE, а не просто индекс: один питомец на пользователя — это правило
    -- контракта (POST /pets отвечает 409, если питомец уже создан), и держать
    -- его должна база, а не проверка в коде, которая проигрывает гонку.
    user_id    UUID        NOT NULL UNIQUE,
    preset_id  TEXT        NOT NULL,
    name       TEXT        NOT NULL,

    hunger     SMALLINT    NOT NULL,
    joy        SMALLINT    NOT NULL,
    clean      SMALLINT    NOT NULL,
    energy     SMALLINT    NOT NULL,
    sleeping   BOOLEAN     NOT NULL DEFAULT FALSE,

    -- Момент, на который посчитаны показатели выше. Контракт называет его
    -- Pet.updatedAt: «точка отсчёта для следующего пересчёта».
    stats_at   TIMESTAMPTZ NOT NULL,

    total_xp   INTEGER     NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Границы показателя — требование контракта («Целые 0..100»), поэтому
    -- проверяет их база. Код, который однажды запишет 120, упадёт здесь, а не
    -- отдаст фронту ответ, не проходящий валидацию по собственной спеке.
    CONSTRAINT pets_stats_in_range CHECK (
        hunger BETWEEN 0 AND 100 AND
        joy    BETWEEN 0 AND 100 AND
        clean  BETWEEN 0 AND 100 AND
        energy BETWEEN 0 AND 100
    ),
    CONSTRAINT pets_total_xp_non_negative CHECK (total_xp >= 0),
    CONSTRAINT pets_name_not_blank CHECK (length(btrim(name)) > 0)
);

-- pet_action_log — журнал действий ухода.
--
-- Это то самое «начислять опыт только вместе с записью в лог» из
-- docs/ARCHITECTURE.md (инвариант 1). Журнал держит сразу три свойства:
--
--  1. Идемпотентность. PRIMARY KEY (user_id, action_id) — action_id приходит
--     с клиента (контракт: «UUID с клиента. Защищает от двойного тапа: повтор
--     возвращает прежний результат»). Повтор не вставляется, а читается.
--  2. Однократность при гонке. Тот же ключ решает и параллельный двойной тап:
--     побеждает ровно одна вставка, вторая получает конфликт. Проверкой в коде
--     это не держится — между SELECT и INSERT помещается второй запрос.
--  3. Суточный кап. day заполняется по таймзоне аккаунта (контракт, правило 6:
--     «Сутки и стрик считаются по timezone аккаунта, а не по времени
--     устройства»), поэтому кап считается суммой по (user_id, day).
--
-- result хранит ответ целиком: повтор обязан вернуть ПРЕЖНИЙ результат, а не
-- пересчитанный на новое время — иначе двойной тап показал бы два разных
-- состояния питомца.
CREATE TABLE pet_action_log (
    user_id    UUID        NOT NULL,
    action_id  UUID        NOT NULL,
    kind       TEXT        NOT NULL,
    xp_granted INTEGER     NOT NULL,
    day        DATE        NOT NULL,
    result     JSONB       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, action_id),

    CONSTRAINT pet_action_log_xp_non_negative CHECK (xp_granted >= 0)
);

-- Индекс под запрос суточного капа: SUM(xp_granted) по пользователю за день.
CREATE INDEX pet_action_log_user_day_idx ON pet_action_log (user_id, day);

-- +goose Down
DROP TABLE pet_action_log;
DROP TABLE pets;
