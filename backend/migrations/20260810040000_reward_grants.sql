-- +goose Up

-- reward_grants — выдача наград. Критический путь (backend/internal/rewards/AGENTS.md):
-- ошибка здесь означает задвоенный грант или награду, которой можно поделиться.
--
-- PRIMARY KEY (user_id, reward_id), а не отдельный id + UNIQUE-индекс: это и
-- есть весь смысл таблицы — на пару (пользователь, награда) может существовать
-- НЕ БОЛЕЕ ОДНОЙ строки, и держать это должна база, а не проверка в коде,
-- которая между SELECT и INSERT проигрывает гонку (см. rewards/AGENTS.md → Never:
-- «не полагайся на проверку «уже выдано?» без UNIQUE-constraint в БД»).
--
-- Ни промокода, ни ссылки на купон здесь нет и не будет (docs/DECISIONS.md → 10.08):
-- награда — право, привязанное к user_id, а не пересылаемая строка.
CREATE TABLE reward_grants (
    user_id         UUID        NOT NULL,
    reward_id       TEXT        NOT NULL,

    -- Idempotency-Key клиента, которым был сделан ИМЕННО этот грант. Повтор
    -- claim с тем же ключом отличают от повторного claim с другим ключом —
    -- первое отдаёт прежний результат (200), второе — 409 REWARD_ALREADY_CLAIMED
    -- (контракт, POST /rewards/{rewardId}/claim).
    idempotency_key UUID        NOT NULL,

    granted_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- used_at — когда награда применена (POST /rewards/{rewardId}/redeem-click).
    -- NULL, пока не применена. В этом MVP переход синхронный (нет интеграции с
    -- настоящим Авито, откуда в проде пришло бы отдельное серверное
    -- событие) — см. docstring redeem-click в docs/openapi.json.
    used_at         TIMESTAMPTZ,

    PRIMARY KEY (user_id, reward_id)
);

-- +goose Down
DROP TABLE reward_grants;
