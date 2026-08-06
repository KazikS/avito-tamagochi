// Точка входа. Пока минимальный HTTP-сервер: только /healthz, никакой логики.
//
// Так сделано, чтобы уже существующая инфраструктура была связной: Dockerfile
// собирает бинарь, docker-compose поднимает контейнер, `make up` его запускает.
// До этого main печатал строку и сразу выходил, поэтому контейнер немедленно
// умирал, а `docker compose up --wait` падал на вышедшем сервисе.
//
// Роутинг сознательно на стандартной библиотеке, а не на Gin: маршруты живут
// в `internal/http/routers` на ветке feat/pet-service, и при её мерже этот файл
// заменяется обычным wiring'ом (роутер + пул Postgres + сервисы). Заводить Gin
// здесь заранее — значит разойтись с той веткой на ровном месте.
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
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Слушателя открываем сами, до старта сервера. Две причины: кривой HTTP_ADDR
	// падает сразу и понятно, а не внутри горутины; и в лог идёт адрес, который
	// вернула ОС (`ln.Addr()`), а не строка из переменной окружения. Логировать
	// сырое значение переменной — это G706 (log injection) у gosec: в переменную
	// можно положить перевод строки и подделать записи в логе.
	ln, lnErr := net.Listen("tcp", addr)
	if lnErr != nil {
		log.Fatalf("не могу слушать %s: %v", srv.Addr, lnErr)
	}

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
