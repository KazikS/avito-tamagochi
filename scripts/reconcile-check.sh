#!/usr/bin/env bash
# Проверяет пункты docs/RECONCILIATION.md по репозиторию, а не по галочкам.
#
# Зачем: чеклист в markdown никто не обновляет — галочка врёт молча, и файл
# живёт дольше работы, которую описывает. Здесь источник правды — код:
# скрипт сам смотрит, исправлено или нет. Когда всё DONE, он говорит удалить
# файл, и это единственный признак конца реконсиляции.
#
# Запуск: make reconcile
set -uo pipefail
cd "$(git rev-parse --show-toplevel)" || exit 1

todo=0 done_=0 skipped=0

# Цвет только в терминале — см. scripts/doctor.sh.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_OK=$'\033[32m'; C_BAD=$'\033[31m'; C_OFF=$'\033[0m'
else
  C_OK=''; C_BAD=''; C_OFF=''
fi

# check <статус> <описание>;  статус: done | todo | na
report() {
  case "$1" in
    done) printf '  %sDONE%s  %s\n' "$C_OK"  "$C_OFF" "$2"; done_=$((done_ + 1)) ;;
    todo) printf '  %sTODO%s  %s\n' "$C_BAD" "$C_OFF" "$2"; todo=$((todo + 1)) ;;
    na)   printf '  ----  %s (ещё не смержено)\n' "$2"; skipped=$((skipped + 1)) ;;
  esac
}

# has <файл> — файл существует
has() { [ -e "$1" ]; }
# grepq <шаблон> <файл> — шаблон найден
grepq() { [ -e "$2" ] && grep -qE "$1" "$2" 2>/dev/null; }

echo "Реконсиляция: состояние по коду (docs/RECONCILIATION.md)"
echo

# --- то, что чинится на этой ветке независимо от мержа ---
if has backend/go.mod; then report done "go.mod переехал в backend/"
else report todo "go.mod всё ещё в корне — переезд первым коммитом реконсиляции"; fi

if has backend/go.mod && grepq 'cd backend && golangci-lint|--config' Makefile; then
  report done "make lint умеет запускать линтер после переезда go.mod"
elif has backend/go.mod; then
  report todo "make lint запускает golangci-lint из корня, где модуля больше нет"
else
  report na "make lint после переезда go.mod"
fi

if grepq 'tamagochi/backend/' .golangci.yaml; then
  if has backend/go.mod; then
    report todo "depguard всё ещё запрещает tamagochi/backend/... — после переезда пути стали tamagochi/..."
  else
    report done "пути depguard соответствуют текущей раскладке"
  fi
else
  report done "пути depguard не ссылаются на устаревший префикс"
fi

n_layers=$(git ls-files | grep -cE 'internal/[^/]+/(service|handler)\.go$')
if [ "$n_layers" -gt 0 ]; then
  report done "целевая раскладка internal/<tag>/{handler,service}.go — файлов: $n_layers"
else
  report todo "depguard не матчит ни одного файла: раскладка ещё не целевая"
fi

# --- то, что приезжает с feat/auth ---
if has backend/internal/repository/user.go; then
  if grepq 'grpc/(codes|status)' backend/internal/repository/user.go; then
    report todo "repository/user.go импортирует grpc ради одного кода ошибки"
  else
    report done "grpc из repository/user.go убран"
  fi
  if grepq 'bcrypt' backend/internal/repository/user.go; then
    report done "пароль хешируется перед вставкой"
  else
    report todo "пароль пишется в БД открытым текстом (нужен bcrypt)"
  fi
else
  report na "repository/user.go: grpc и хеширование пароля"
fi

if has backend/internal/pet/models/user.go; then
  report todo "User лежит в пакете pet — не его домен"
elif has backend/internal/models/user.go; then
  report done "User переехал из пакета pet"
else
  report na "расположение модели User"
fi

mig=$(ls backend/migrations/*.sql 2>/dev/null | head -1)
if [ -n "$mig" ]; then
  if grepq 'password[[:space:]]+TEXT[[:space:]]+NOT NULL' "$mig"; then
    report done "колонка password объявлена NOT NULL"
  else
    report todo "колонка password без NOT NULL — схема разрешает пустой пароль"
  fi
else
  report na "миграция users (приезжает с feat/auth)"
fi

# --- то, что приезжает с feat/pet-service ---
if has backend/internal/usecase/pet.go; then
  if go build ./... >/dev/null 2>&1; then
    report done "дерево собирается (у GetPetState есть тело)"
  else
    report todo "сборка красная — вероятно, у GetPetState нет тела функции"
  fi
else
  report na "usecase/pet.go: тело GetPetState"
fi

rt=backend/internal/http/routers/routers.go
if has "$rt"; then
  grepq 'POST\("/action"\)[[:space:]]*$' "$rt" \
    && report todo "pet.POST(\"/action\") зарегистрирован без хендлера — паника при первом запросе" \
    || report done "у POST /action есть хендлер"
  grepq 'GET\("/"' "$rt" \
    && report todo "роут GET \"/\" без :id, а хендлер читает c.Param(\"id\")" \
    || report done "роут питомца объявлен с параметром пути"
else
  report na "роуты питомца (приезжают с feat/pet-service)"
fi

# --- контракт и инфраструктура ---
if grepq '"password"' docs/openapi.json; then
  report done "openapi.json описывает login+password (решение 06.08)"
else
  report todo "openapi.json всё ещё описывает вход по nickname — расходится с решением 06.08"
fi

dc=backend/deployments/docker-compose.yaml
grepq 'POSTGRES_PASSWORD:?=?[[:space:]]*1234' "$dc" \
  && report todo "POSTGRES_PASSWORD=1234 лежит в docker-compose.yaml" \
  || report done "пароль Postgres не захардкожен в compose"
grepq 'beers_data' "$dc" \
  && report todo "путь тома ./pgdata/beers_data — копипаста из чужого проекта" \
  || report done "путь тома Postgres не из чужого проекта"
grepq '^[[:space:]]+postgres:' "$dc" \
  && report done "в compose есть сервис postgres" \
  || report todo "в compose нет сервиса postgres (он только в feat/auth)"

echo
echo "DONE: $done_   TODO: $todo   ещё не смержено: $skipped"
if [ "$todo" -eq 0 ] && [ "$skipped" -eq 0 ]; then
  echo
  echo "Всё закрыто — удали docs/RECONCILIATION.md, это и есть конец реконсиляции:"
  echo "  git rm docs/RECONCILIATION.md && git commit -m 'chore: реконсиляция завершена'"
fi
exit 0
