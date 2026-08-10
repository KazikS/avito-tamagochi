# Node LTS. Версия не зафиксирована в package.json (нет engines) — берём
# ту же линию, что уже используют сгенерированные @chakra-ui/cli типы
# (требуют Node 20+); 22 — текущий LTS на момент написания.
FROM node:22-alpine AS builder

WORKDIR /src

# Копируем ВЕСЬ исходник, не только package.json/package-lock.json (обычный
# приём кэширования слоёв здесь ломается): postinstall (`npm run chakra`,
# package.json) генерирует типы из src/shared/ui/theme/theme.ts — npm ci
# упадёт на постустановочном скрипте, если исходника ещё нет на диске.
# Cлой package.json/package-lock.json отдельно не кэшируется, зато сборка
# вообще проходит.
COPY frontend/ ./
RUN npm ci
RUN npm run build

FROM nginx:alpine

# Проксирует /api, /debug, /ws на бэкенд-сервис (см. docker-compose.yaml) —
# то же самое, что делает vite-прокси в dev (frontend/vite.config.ts), но
# для собранного статического бандла: у него уже нет dev-сервера, который
# мог бы проксировать сам.
COPY frontend/build/package/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /src/dist /usr/share/nginx/html

EXPOSE 80
