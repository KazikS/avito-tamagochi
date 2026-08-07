package wsh_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"tamagochi/pkg/wsh"
)

// dial поднимает настоящий WebSocket и отдаёт обе стороны.
//
// Не фейк и не подменённый интерфейс: проверяемые свойства — про поведение
// gorilla (паника при двух пишущих горутинах, ошибка записи в закрытое
// соединение), и на заглушке они не воспроизводятся вовсе.
func dial(t *testing.T) (server, client *websocket.Conn) {
	t.Helper()

	var upgrader websocket.Upgrader
	accepted := make(chan *websocket.Conn, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(srv.Close)

	client, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	// Ответ на рукопожатие приходит и при успехе (101), и при отказе; тело
	// надо закрыть в обоих случаях, иначе соединение утекает.
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("не удалось подключиться: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case server = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("сервер не принял соединение за 5 секунд")
	}
	t.Cleanup(func() { _ = server.Close() })

	return server, client
}

// mustAdd регистрирует соединение и валит тест, если это не удалось.
func mustAdd(t *testing.T, h *wsh.Hub, userID uuid.UUID, conn *websocket.Conn) *wsh.Conn {
	t.Helper()
	c, err := h.Add(userID, conn)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	return c
}

// readOne читает один кадр с клиентской стороны под дедлайном.
func readOne(t *testing.T, client *websocket.Conn) string {
	t.Helper()
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, payload, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	return string(payload)
}

// Нулевой пользователь — не пользователь. Именно возможность зарегистрировать
// соединение под пустым ключом делает адресный пуш невозможным, ничего при
// этом не ломая заметно.
func TestAddRejectsNilUser(t *testing.T) {
	server, _ := dial(t)

	if _, err := wsh.NewHub().Add(uuid.Nil, server); !errors.Is(err, wsh.ErrNoUser) {
		t.Errorf("получили %v, ожидали ErrNoUser", err)
	}
}

func TestAddRejectsNilConn(t *testing.T) {
	if _, err := wsh.NewHub().Add(uuid.New(), nil); !errors.Is(err, wsh.ErrNoConn) {
		t.Errorf("получили %v, ожидали ErrNoConn", err)
	}
}

func TestSendDelivers(t *testing.T) {
	server, client := dial(t)
	hub := wsh.NewHub()
	user := uuid.New()
	mustAdd(t, hub, user, server)

	sent, err := hub.Send(user, map[string]string{"type": "pet.state"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent != 1 {
		t.Fatalf("доставлено %d, ожидали 1", sent)
	}

	if got := readOne(t, client); got != `{"type":"pet.state"}` {
		t.Errorf("получили %s", got)
	}
}

// Две вкладки одного пользователя — обычное дело. map[userID]*Conn молча терял
// бы первую: сообщение уходило бы только в последнюю открытую.
func TestSendReachesEveryConnectionOfUser(t *testing.T) {
	serverA, clientA := dial(t)
	serverB, clientB := dial(t)

	hub := wsh.NewHub()
	user := uuid.New()
	mustAdd(t, hub, user, serverA)
	mustAdd(t, hub, user, serverB)

	if got := hub.Online(user); got != 2 {
		t.Fatalf("онлайн %d, ожидали 2", got)
	}

	sent, err := hub.Send(user, map[string]int{"seq": 1})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent != 2 {
		t.Fatalf("доставлено %d, ожидали 2", sent)
	}

	for name, client := range map[string]*websocket.Conn{"A": clientA, "B": clientB} {
		if got := readOne(t, client); got != `{"seq":1}` {
			t.Errorf("вкладка %s получила %s", name, got)
		}
	}
}

// Сообщение одного пользователя не должно попадать другому. Это и есть то
// свойство, которое сломано в feat/pet-service: ключом там служит
// идентификатор запроса, а не пользователя.
func TestSendDoesNotLeakToOtherUsers(t *testing.T) {
	serverA, _ := dial(t)
	serverB, clientB := dial(t)

	hub := wsh.NewHub()
	alice, bob := uuid.New(), uuid.New()
	mustAdd(t, hub, alice, serverA)
	mustAdd(t, hub, bob, serverB)

	if _, err := hub.Send(alice, map[string]string{"для": "alice"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if err := clientB.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, _, err := clientB.ReadMessage(); err == nil {
		t.Error("соединение bob получило сообщение, адресованное alice")
	}
}

func TestSendToUnknownUser(t *testing.T) {
	sent, err := wsh.NewHub().Send(uuid.New(), map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent != 0 {
		t.Errorf("доставлено %d, ожидали 0", sent)
	}
}

// Несериализуемая нагрузка — ошибка вызывающего, а не отвал соединения:
// возвращаем её, ничего никому не отправив.
func TestSendReportsMarshalFailure(t *testing.T) {
	server, _ := dial(t)
	hub := wsh.NewHub()
	user := uuid.New()
	mustAdd(t, hub, user, server)

	sent, err := hub.Send(user, make(chan int))
	if err == nil {
		t.Fatal("ожидали ошибку сериализации")
	}
	if sent != 0 {
		t.Errorf("доставлено %d при ошибке сериализации, ожидали 0", sent)
	}
	if hub.Online(user) != 1 {
		t.Error("ошибка сериализации сняла соединение с регистрации")
	}
}

// Мёртвое соединение снимается с регистрации самой попыткой записи: иначе хаб
// растёт до перезапуска процесса.
func TestSendDropsDeadConnection(t *testing.T) {
	server, _ := dial(t)
	hub := wsh.NewHub()
	user := uuid.New()
	mustAdd(t, hub, user, server)

	if err := server.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sent, err := hub.Send(user, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent != 0 {
		t.Errorf("доставлено %d в закрытое соединение, ожидали 0", sent)
	}
	if got := hub.Online(user); got != 0 {
		t.Errorf("после отвала осталось %d соединений, ожидали 0", got)
	}
}

// Remove зовут из defer обработчика и из Send, обнаружившего мёртвого пира;
// порядок между ними не определён, поэтому повторный вызов обязан быть
// безобидным. В feat/pet-service тот же метод читает карту вне блокировки и
// делает Close по nil, если ключа нет.
func TestRemoveIsIdempotentAndNilSafe(t *testing.T) {
	server, _ := dial(t)
	hub := wsh.NewHub()
	user := uuid.New()
	conn := mustAdd(t, hub, user, server)

	hub.Remove(user, conn)
	hub.Remove(user, conn)
	hub.Remove(user, nil)
	hub.Remove(uuid.New(), conn)

	if got := hub.Online(user); got != 0 {
		t.Errorf("осталось %d соединений, ожидали 0", got)
	}
}

// gorilla допускает не больше одной пишущей горутины на соединение. Без
// мьютекса внутри Conn этот тест падает с паникой
// «concurrent write to websocket connection».
func TestConcurrentSendIsSerialised(t *testing.T) {
	serverA, clientA := dial(t)
	serverB, clientB := dial(t)

	hub := wsh.NewHub()
	user := uuid.New()
	mustAdd(t, hub, user, serverA)
	mustAdd(t, hub, user, serverB)

	// Читатели нужны, чтобы буфер сокета не заполнился и записи не упёрлись
	// в дедлайн: проверяем сериализацию записи, а не поведение под затором.
	var readers sync.WaitGroup
	done := make(chan struct{})
	for _, client := range []*websocket.Conn{clientA, clientB} {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				if _, _, err := client.ReadMessage(); err != nil {
					return
				}
				select {
				case <-done:
					return
				default:
				}
			}
		}()
	}

	const writers, messages = 8, 50

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range messages {
				if _, err := hub.Send(user, map[string]int{"w": w, "m": m}); err != nil {
					t.Errorf("Send: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	close(done)
	hub.Close()
	readers.Wait()

	if got := hub.Online(user); got != 0 {
		t.Errorf("после Close осталось %d соединений", got)
	}
}

// Close обязан именно закрыть сокеты, а не только очистить карту: иначе
// соединения переживут остановку сервера.
func TestCloseClosesSockets(t *testing.T) {
	server, client := dial(t)
	hub := wsh.NewHub()
	user := uuid.New()
	mustAdd(t, hub, user, server)

	hub.Close()

	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, _, err := client.ReadMessage(); err == nil {
		t.Error("клиент продолжает читать из соединения после Close")
	}
}

// После Close хаб пригоден к использованию: это упрощает и тесты, и повторную
// сборку сервера, и не оставляет ловушки «использование после закрытия».
func TestHubIsReusableAfterClose(t *testing.T) {
	serverA, _ := dial(t)
	hub := wsh.NewHub()
	user := uuid.New()
	mustAdd(t, hub, user, serverA)

	hub.Close()

	serverB, clientB := dial(t)
	mustAdd(t, hub, user, serverB)

	sent, err := hub.Send(user, map[string]string{"после": "close"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent != 1 {
		t.Fatalf("доставлено %d, ожидали 1", sent)
	}
	if got := readOne(t, clientB); got != `{"после":"close"}` {
		t.Errorf("получили %s", got)
	}
}
