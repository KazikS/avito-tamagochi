# Реконсиляция веток

Временный файл. `feat/auth` и `feat/pet-service` вливаются как есть, дальше правится
здесь же. **Когда список пуст — файл удаляется**, и это единственный признак того, что
реконсиляция закончена. Не переносить пункты в `DECISIONS.md`: там решения, а не работа.

⚠️ Галочкам ниже не верь — верь `make reconcile`. Он проверяет каждый пункт по коду
(есть ли `bcrypt`, импортируется ли `grpc`, матчит ли `depguard` хоть один файл) и
печатает DONE/TODO. Список в markdown нужен, чтобы объяснить *почему*; что именно
осталось — знает скрипт, и он не устаревает молча.

Это чеклист, а не претензия к авторам веток: половина пунктов появилась из-за того, что
ветки разошлись, а не из-за чьей-то ошибки.

## Как мержится (проверено в скретч-ветке)

Порядок: сначала `feat/auth`, потом `feat/pet-service`.

Конфликты, которые будут:

| Где | Что | Как решать |
|---|---|---|
| `pkg/postgres/postgres.go` | доккомментарий пакета | взять версию из ветки |
| `internal/pet/models/user.go` | файл лежит в каталоге, который вторая ветка переименовала целиком | оставить по новому пути (`internal/models/user.go`) |
| `go.mod`, `go.sum` | несовместимые деревья зависимостей (grpc против gin) | не править руками: снять grpc, затем `go mod tidy` |

После этих правок дерево собирается — проверено `go build ./...`.

## Чеклист

- [ ] `usecase/pet.go` — у `GetPetState` нет тела функции, `feat/pet-service` не собирается
- [ ] `GetPetInfo` читает `c.Param("id")`, а роут зарегистрирован как `/` без `:id`
- [ ] `pet.POST("/action")` — роут без хендлера, паника при первом запросе
- [ ] `repository/user.go` импортирует `grpc/{codes,status}` ради одного кода ошибки:
      заменить на sentinel, gRPC-сервера в проекте нет
- [ ] пароль пишется в БД открытым текстом, колонка `password` без `NOT NULL` —
      bcrypt перед вставкой + констрейнт
- [x] `docs/openapi.json` приведён к решению `login` + `password` (06.08).
      `nickname` остался как отображаемое имя в `Me` и лидерборде — это не логин
- [ ] `User` лежит в пакете `pet` — не его домен
- [x] `go.mod` переехал в `backend/` (сделано и здесь, и в `feat/pet-service`)
- [x] после переезда: `make lint`, `.golangci.yaml` (пути depguard), CI
      (`cache-dependency-path`), Dockerfile, `doctor.sh`, `reconcile-check.sh`
- [ ] **`feat/pet-service` не собирается**: там `go.mod` переехал, а импорты остались
      `tamagochi/backend/...`. Чинится заменой на `tamagochi/...` в 4 файлах
- [ ] **две миграции создают таблицу `users` по-разному** и вторая упадёт
      («relation already exists»): `feat/auth` — `id SERIAL, login, password`,
      `feat/pet-service` — `id UUID, username, password_hash`. Нужна одна схема.
      Заодно: `repository/user.go` пишет `INSERT INTO users (login, password)`,
      то есть код и схема `feat/pet-service` расходятся по именам колонок
- [ ] `internal/websocket` из `feat/pet-service` не чинить по месту, а заменить на
      `pkg/wsh`: там уже закрыты все три дефекта — `RemoveClient` читал карту вне
      блокировки и делал `conn.Close()` по nil; ключом служил request ID
      (`RequestIDKey` и `requestIDKey` — одно значение), а не user ID; записи
      в соединение не были сериализованы, хотя gorilla допускает одну пишущую
      горутину. Каждый из трёх закрыт тестом, который краснеет при откате правки

## Куда переезжает `feat/pet-service`

Раскладка ветки старше решения от 06.08 о целевой (`docs/ARCHITECTURE.md`), поэтому
мерж — это переименования, а не переписывание. Соответствие файлов:

| `feat/pet-service` | цель | что меняется по смыслу |
|---|---|---|
| `internal/usecase/pet.go` | `internal/pet/service.go` | ничего, кроме пакета; `time.Now()` внутри домена запрещён — часы приходят из `pkg/clock` |
| `internal/repository/pet.go`, `progress.go` | `internal/pet/repo.go` | единственное место с SQL |
| `internal/http/handlers/pet.go` | `internal/pet/handler.go` | ответы через `internal/httpx`, а не `gin.H{"error": ...}`: конверт контракта клиент разбирает, `gin.H` — нет |
| `internal/http/handlers/websocket.go` | `internal/pet/handler.go` | идентификатор пользователя из `pkg/authctx`, соединения — в `pkg/wsh` |
| `internal/websocket/manager.go` | `pkg/wsh` | удаляется: глобальный `Manager` заменён хабом, который передаётся явно |
| `internal/models/pet.go` | `internal/pet/` или типы контракта | формы запросов и ответов берутся из `internal/api`, руками не дублируются |
| `internal/http/routers/routers.go` | `cmd/wire.go` | пакет фичи отдаёт `Register(gin.IRoutes)`, префикс `/api/v1` вешает `cmd/` |

Импорты при этом чинятся сами: `tamagochi/backend/...` → `tamagochi/...`.

По `go.mod` конфликт должен быть меньше, чем описано выше в таблице: `gin` и
`gorilla/websocket` запинены здесь ровно тех версий, что в ветке (`v1.12.0` и
`v1.5.3`), поэтому их строки совпадут дословно. `pgx/v5` здесь **не** зависимость
— ни один пакет пока не ходит в базу, — и приедет он с той ветки, которая первой
напишет `repo.go`; в `feat/auth` и `feat/pet-service` это одна и та же версия
`v5.10.0`, так что между собой они тоже не разойдутся.
- [ ] свести две раскладки к целевой (`docs/ARCHITECTURE.md`)
- [x] проверено, что `depguard` краснеет на нарушении слоя (прогон на временных
      файлах). Осталось, чтобы под правила попал настоящий код — см. пункт выше
- [x] `docker-compose.yaml`: сервис `postgres` добавлен здесь — с паролем из
      переменной окружения, именованным томом и healthcheck. При мерже `feat/auth`
      конфликт по этому файлу решать в пользу нашей версии
- [x] `POSTGRES_PASSWORD=1234` и путь тома `./pgdata/beers_data` из `feat/auth`
      сюда не переезжают: наша версия compose их не содержит
