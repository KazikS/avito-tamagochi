// Команда seed наполняет базу демо-стенда правдоподобными данными: несколько
// питомцев на разных днях активности, чтобы `GET /leaderboard` и
// `GET /summary/daily` на демо показывали живую картину, а не пустой список
// или единственную строку.
//
// Проводит действия ЧЕРЕЗ настоящий internal/pet.Service (не INSERT SQL
// напрямую): так же, как это уже решено для гонки на claim в
// docs/DECISIONS.md — «мок не доказывает» — сырые INSERT не доказывают,
// что результат пройдёт через реальные суточные лимиты и распад, и уже
// один раз в этом проекте расхождение ручного JSON с тем, что пишет домен,
// оказалось багом (см. internal/social/repo.go → Day, докстринг). Тот же
// сервис, тот же путь, что и у настоящего клиента.
//
// Идемпотентна: actionId каждого действия — детерминированный UUID v5 от
// (пользователь, день, вид действия, номер в раунде), поэтому повторный
// `make seed` не начисляет опыт дважды — идёт по тому же пути идемпотентности
// (PRIMARY KEY(user_id, action_id) в pet_action_log), что и повтор с
// настоящего клиента при плохой сети.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"

	"tamagochi/internal/config"
	"tamagochi/internal/pet"
	"tamagochi/pkg/clock"
	"tamagochi/pkg/postgres"
)

// demoUserID — тот же самый пользователь, что видит демо-стенд без заголовка
// X-Demo-User-Id (backend/cmd/demo_identity.go). Здесь отдельная копия
// константы, не импорт: cmd/seed — второй бинарь в модуле (свой package main),
// а demoUserID живёт в package main пакета cmd/. Изменится один — поменяй
// оба: две строки дешевле нового пакета ради одной константы.
var demoUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// seedNamespace — пространство имён для детерминированных actionId (UUID v5).
// Свой, не uuid.NameSpaceURL/DNS: у сида нет ничего общего с внешними
// идентификаторами, а фиксированная константа — единственное, что нужно для
// того, чтобы один и тот же вызов сида давал один и тот же UUID при каждом
// запуске.
var seedNamespace = uuid.MustParse("c0ffee00-0000-0000-0000-000000000000")

func actionID(userID uuid.UUID, daysAgo int, kind pet.ActionKind, n int) uuid.UUID {
	key := fmt.Sprintf("%s|%d|%s|%d", userID, daysAgo, kind, n)
	return uuid.NewSHA1(seedNamespace, []byte(key))
}

// seedUser описывает одного демо-пользователя: чем больше activeDays, тем
// дольше история ухода и тем выше место в недельном лидерборде — разброс
// нужен, чтобы демо показывало настоящий порядок, а не одну строку.
type seedUser struct {
	id         uuid.UUID
	name       string
	preset     string
	activeDays int
}

var seedUsers = []seedUser{
	{demoUserID, "Демо", "blue", 6},
	{uuid.MustParse("00000000-0000-0000-0000-000000000002"), "Егор", "green", 5},
	{uuid.MustParse("00000000-0000-0000-0000-000000000003"), "Марина", "orange", 4},
	{uuid.MustParse("00000000-0000-0000-0000-000000000004"), "Тихон", "purple", 2},
	{uuid.MustParse("00000000-0000-0000-0000-000000000005"), "Аня", "pink", 0},
}

// kinds — порядок действий внутри одного дня. Каждое ровно один раз: суточные
// лимиты у каждого вида от 2 до 6 (pet.DefaultEconomy), одного раунда в день
// достаточно, чтобы не упереться ни в один из них случайно.
var kinds = []pet.ActionKind{pet.ActionFeed, pet.ActionPlay, pet.ActionWash, pet.ActionSleep, pet.ActionWake}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("не задан DATABASE_URL")
	}

	// startupCtx — короткий, только на миграцию и открытие пула, и явно
	// отменяется на каждом Fatal-пути (log.Fatal зовёт os.Exit, defer этой
	// функции по такому пути не отработает — gocritic: exitAfterDefer, тот же
	// приём, что и в cmd/main.go). Имя отдельное от ctx ниже НАМЕРЕННО: тот
	// же баг уже был здесь однажды — общий ctx, отменённый после старта,
	// убивал каждый следующий вызов Service.Act «context canceled».
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)

	if migrateErr := postgres.Migrate(startupCtx, dsn); migrateErr != nil {
		cancelStartup()
		log.Fatalf("не могу накатить миграции: %v", migrateErr)
	}
	pool, poolErr := postgres.New(startupCtx, dsn)
	if poolErr != nil {
		cancelStartup()
		log.Fatalf("не могу подключиться к базе: %v", poolErr)
	}
	cancelStartup()

	clk := clock.NewFixed(time.Now())
	svc, svcErr := pet.NewService(pet.NewRepo(pool), clk, pet.DefaultEconomy, config.DefaultCurve, config.DefaultDailyCareXPCap)
	if svcErr != nil {
		pool.Close()
		log.Fatalf("сборка сервиса pet: %v", svcErr)
	}

	// ctx живёт весь сид, отдельно от startupCtx выше. Без defer: цикл ниже
	// тоже может завершиться Fatal-выходом (exitAfterDefer) — пул закрывается
	// явно на обоих исходах.
	ctx := context.Background()
	realNow := time.Now()
	for _, u := range seedUsers {
		if err := seedOne(ctx, svc, clk, realNow, u); err != nil {
			pool.Close()
			log.Fatalf("сидирование %s: %v", u.name, err)
		}
		log.Printf("готово: %s (%s), %d дн. активности", u.name, u.id, u.activeDays+1)
	}

	pool.Close()
	log.Printf("засеяно пользователей: %d", len(seedUsers))
}

// anchor — 9:00 UTC в дне daysAgo дней назад от реального «сейчас». Фиксированный
// час, один и тот же для создания питомца и для каждого раунда действий:
// разные пользователи и разные дни должны быть сравнимы друг с другом, а не
// зависеть от того, в какую секунду реально запустили сид.
func anchor(realNow time.Time, daysAgo int) time.Time {
	y, m, d := realNow.AddDate(0, 0, -daysAgo).Date()
	return time.Date(y, m, d, 9, 0, 0, 0, time.UTC)
}

func seedOne(ctx context.Context, svc *pet.Service, clk *clock.Fixed, realNow time.Time, u seedUser) error {
	actor := pet.Actor{UserID: u.id}

	clk.Set(anchor(realNow, u.activeDays))
	if _, err := svc.Create(ctx, actor, u.preset, u.name); err != nil && !errors.Is(err, pet.ErrPetExists) {
		return fmt.Errorf("создание питомца: %w", err)
	}

	for daysAgo := u.activeDays; daysAgo >= 0; daysAgo-- {
		clk.Set(anchor(realNow, daysAgo))
		for i, kind := range kinds {
			id := actionID(u.id, daysAgo, kind, i)
			if _, _, err := svc.Act(ctx, actor, id, kind); err != nil {
				return fmt.Errorf("действие %s (−%d дн.): %w", kind, daysAgo, err)
			}
		}
	}
	return nil
}
