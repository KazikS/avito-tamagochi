// Точка входа: процесс, слушатель, останов по сигналу и накат миграций.
// Роутов здесь намеренно нет — их собирает wire.go, это самый конфликтный файл.
//
// Сервер поднимается по-настоящему, а не печатает строку и выходит, чтобы
// инфраструктура была связной: Dockerfile собирает бинарь, docker-compose
// поднимает контейнер, `make up` его запускает. Раньше main выходил сразу,
// поэтому контейнер немедленно умирал, а `docker compose up --wait` падал
// на вышедшем сервисе.
//
// Роутер на Gin — так записано в docs/DECISIONS.md («В коде») и так написан
// feat/pet-service; сборка маршрутов живёт в cmd/wire.go, чтобы мерж чужой
// ветки трогал этот файл минимально.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tamagochi/pkg/postgres"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// DATABASE_URL обязателен: с приездом internal/pet роутеру нечем работать
	// без базы (см. комментарий у newRouter в wire.go), а docker-compose.yaml
	// уже прокидывает эту переменную и держит postgres как зависимость с
	// healthcheck. Падаем здесь, а не при первом запросе к /pet.
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("не задан DATABASE_URL")
	}

	// Контекст на миграции и открытие пула — короткий и не связан с временем
	// жизни сервера: ждать миграцию до сигнала останова незачем.
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

	// Роутер собирается до слушателя: ошибка в константах экономики должна
	// ронять процесс на старте, а не отдавать 500 на первом запросе.
	// Имя routerErr, а не err: ниже по функции err объявляют внутри if'ов, и
	// функциональная переменная err их бы затеняла (govet shadow, strict).
	// Тот же приём уже применён ниже для lnErr.
	router, routerErr := newRouter(pool)
	if routerErr != nil {
		pool.Close()
		log.Fatalf("не могу собрать роутер: %v", routerErr)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Слушателя открываем сами, до старта сервера. Две причины: кривой HTTP_ADDR
	// падает сразу и понятно, а не внутри горутины; и в лог идёт адрес, который
	// вернула ОС (`ln.Addr()`), а не строка из переменной окружения. Логировать
	// сырое значение переменной — это G706 (log injection) у gosec: в переменную
	// можно положить перевод строки и подделать записи в логе.
	ln, lnErr := net.Listen("tcp", addr)
	if lnErr != nil {
		pool.Close()
		log.Fatalf("не могу слушать %s: %v", srv.Addr, lnErr)
	}

	// defer pool.Close() — только теперь, когда выше не осталось ни одного
	// Fatal-пути: log.Fatal зовёт os.Exit, defer'ы этой функции по такому пути
	// не отработают (gocritic: exitAfterDefer), поэтому на каждом Fatal-пути
	// выше пул закрывается явно, а не через defer. Ниже Fatal больше нет —
	// значит от этой строки и до конца функции defer действительно сработает.
	defer pool.Close()

	// Останов по сигналу: без него контейнер убивают по таймауту на каждом
	// docker compose down, и это заметно замедляет цикл разработки.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("сервер слушает %v", ln.Addr())
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("сервер остановлен с ошибкой: %v", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("неаккуратный останов: %v", err)
	}
}
