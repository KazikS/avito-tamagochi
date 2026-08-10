package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"tamagochi/migrations"
)

// progressTables — таблицы, где живёт прогресс пользователя: total_xp и
// показатели питомца (pets), выданные награды (reward_grants), журнал
// действий, от которого зависит идемпотентность и суточный кап
// (pet_action_log). Список расширять по мере появления новых таблиц с
// прогрессом, не по мере появления новых миграций.
var progressTables = []string{"pets", "reward_grants", "pet_action_log"}

// dangerous — операции, которые в одном SQL-выражении с таблицей прогресса
// либо уничтожают, либо обнуляют данные. UPDATE ловится тоже: миграция,
// которая переписывает total_xp/hunger и т. п., так же нарушает инвариант,
// как DROP.
var dangerous = regexp.MustCompile(`(?i)\b(DROP\s+COLUMN|DROP\s+TABLE|TRUNCATE|UPDATE)\b`)

// invariant:6 — прогресс пользователя не обнуляется миграцией
// (docs/ARCHITECTURE.md). Статическая проверка Up-секции каждой миграции по
// тексту, не прогон против настоящей базы: если какое-то SQL-выражение в
// Up одновременно называет таблицу прогресса и несёт одну из dangerous-
// операций, миграция может обнулить или уничтожить данные пользователя.
//
// Down-секции этой проверкой не покрыты и не должны быть: DROP TABLE в Down
// — это ровно то, что Down обязан делать, откатывая Up.
func TestMigrationsNeverResetProgress(t *testing.T) {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("чтение embed.FS: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		found = true

		raw, readErr := migrations.FS.ReadFile(e.Name())
		if readErr != nil {
			t.Fatalf("чтение %s: %v", e.Name(), readErr)
		}

		for _, stmt := range strings.Split(upSection(string(raw)), ";") {
			if !dangerous.MatchString(stmt) {
				continue
			}
			lower := strings.ToLower(stmt)
			for _, table := range progressTables {
				if strings.Contains(lower, table) {
					t.Errorf("%s: Up-секция несёт опасную операцию над %q: %q — миграция может обнулить прогресс пользователя",
						e.Name(), table, strings.TrimSpace(stmt))
				}
			}
		}
	}
	if !found {
		t.Fatal("миграций в embed.FS не найдено — тест ничего не проверил")
	}
}

// upSection вырезает текст между "-- +goose Up" и "-- +goose Down" (или до
// конца файла, если Down нет).
func upSection(raw string) string {
	upStart := strings.Index(raw, "-- +goose Up")
	if upStart == -1 {
		return raw
	}
	rest := raw[upStart:]
	if downStart := strings.Index(rest, "-- +goose Down"); downStart != -1 {
		return rest[:downStart]
	}
	return rest
}
