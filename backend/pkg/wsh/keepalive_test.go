package wsh_test

import (
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"tamagochi/pkg/wsh"
)

// KeepAlive возвращается, когда клиент закрывает соединение штатно.
func TestKeepAliveReturnsOnClientClose(t *testing.T) {
	server, client := dial(t)

	done := make(chan struct{})
	go func() {
		wsh.KeepAlive(server, time.Second)
		close(done)
	}()

	if err := client.Close(); err != nil {
		t.Fatalf("закрытие клиента: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("KeepAlive не вернулась за 5 секунд после закрытия клиента")
	}
}

// Дедлайн чтения — настоящее время, а не что-то, что можно подменить снаружи:
// не отвечающий пингом клиент обязан быть отброшен по истечении interval,
// а не висеть, пока кто-то не закроет соединение руками.
//
// Это ровно то свойство, которое сломала первая версия serveWS в
// internal/pet/ws.go, читавшая дедлайн через доменные часы теста (см.
// докстринг KeepAlive) — с ними реальный сокет получал дедлайн в прошлом
// или в произвольном будущем вместо «interval с настоящего момента».
func TestKeepAliveEnforcesRealDeadline(t *testing.T) {
	server, client := dial(t)
	t.Cleanup(func() { _ = client.Close() })

	const interval = 200 * time.Millisecond
	start := time.Now()

	done := make(chan struct{})
	go func() {
		wsh.KeepAlive(server, interval)
		close(done)
	}()

	// Клиент не шлёт ни pong, ни сообщений — сервер обязан сам отвалиться
	// по дедлайну, никем не подталкиваемый.
	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed < interval {
			t.Fatalf("KeepAlive вернулась через %v, раньше interval %v — дедлайн не выдержан", elapsed, interval)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("KeepAlive не вернулась за 2 секунды при отсутствующем клиенте — дедлайн не сработал")
	}
}

// Входящие сообщения (в этом проекте — только ping/pong, обрабатываемые
// gorilla на уровне протокола) не роняют цикл: KeepAlive продолжает читать,
// пока соединение действительно не оборвётся.
func TestKeepAliveSurvivesIncomingMessages(t *testing.T) {
	server, client := dial(t)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wsh.KeepAlive(server, 2*time.Second)
	}()

	for range 3 {
		if err := client.WriteMessage(websocket.TextMessage, []byte("клиент не должен слать текст по контракту, но и это не должно ронять цикл")); err != nil {
			t.Fatalf("запись с клиента: %v", err)
		}
	}

	if err := client.Close(); err != nil {
		t.Fatalf("закрытие клиента: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("KeepAlive не вернулась после закрытия клиента, хотя тот успел прислать сообщения")
	}
}
