// Package wsh — реестр WebSocket-соединений, сгруппированных по пользователю.
//
// Заменяет глобальный ClientManager из feat/pet-service и чинит три его
// свойства, каждое из которых ломает адресный пуш состояния питомца:
//
//  1. Ключ — uuid.UUID, а не строка из контекста. Пустой ключ построить нельзя:
//     Add отвергает uuid.Nil (см. также pkg/authctx).
//  2. На пользователя может быть НЕСКОЛЬКО соединений: две вкладки — обычное
//     дело, а map[string]*websocket.Conn молча терял предыдущее.
//  3. Запись в соединение сериализована мьютексом. gorilla/websocket
//     допускает не больше одной пишущей горутины на соединение; без мьютекса
//     параллельный пуш двум вкладкам одного пользователя даёт панику
//     «concurrent write to websocket connection».
//
// Разделение обязанностей с обработчиком: читает из соединения обработчик
// (у него остаётся *websocket.Conn, который он передал в Add), пишет — только
// хаб. Это ровно та модель, которой требует gorilla: одна пишущая горутина и
// одна читающая.
package wsh

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// writeTimeout — предельное время на одну запись. Без дедлайна мёртвый пир
// (закрытый ноутбук, пропавший Wi-Fi) держит горутину пуша навсегда.
const writeTimeout = 5 * time.Second

// ErrNoUser возвращается, когда в Add передали нулевой идентификатор.
var ErrNoUser = errors.New("wsh: нулевой идентификатор пользователя")

// ErrNoConn возвращается, когда в Add передали nil-соединение.
var ErrNoConn = errors.New("wsh: nil-соединение")

// Conn — одно зарегистрированное соединение.
type Conn struct {
	ws *websocket.Conn

	// mu сериализует запись: требование gorilla, см. доккомментарий пакета.
	mu sync.Mutex

	// once делает закрытие идемпотентным. Закрыть соединение могут и Remove
	// из defer обработчика, и Send, обнаруживший мёртвого пира, и Close при
	// остановке сервера — в любом порядке и одновременно.
	once sync.Once
}

// send пишет один кадр. Ошибка означает, что соединение больше не живо.
func (c *Conn) send(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ws.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	return c.ws.WriteMessage(websocket.TextMessage, payload)
}

// close закрывает соединение ровно один раз.
func (c *Conn) close() {
	c.once.Do(func() { _ = c.ws.Close() })
}

// Hub хранит соединения по пользователям.
type Hub struct {
	mu    sync.RWMutex
	conns map[uuid.UUID]map[*Conn]struct{}
}

// NewHub создаёт пустой хаб.
func NewHub() *Hub {
	return &Hub{conns: make(map[uuid.UUID]map[*Conn]struct{})}
}

// Add регистрирует соединение за пользователем и возвращает дескриптор,
// который нужно передать в Remove при отключении.
func (h *Hub) Add(userID uuid.UUID, ws *websocket.Conn) (*Conn, error) {
	if userID == uuid.Nil {
		return nil, ErrNoUser
	}
	if ws == nil {
		return nil, ErrNoConn
	}

	c := &Conn{ws: ws}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[userID] == nil {
		h.conns[userID] = make(map[*Conn]struct{})
	}
	h.conns[userID][c] = struct{}{}
	return c, nil
}

// Remove снимает соединение с регистрации и закрывает его.
//
// Идемпотентна и безопасна для nil: обработчик зовёт её из defer, а Send —
// когда запись не прошла, и порядок между ними не определён.
func (h *Hub) Remove(userID uuid.UUID, c *Conn) {
	if c == nil {
		return
	}

	h.mu.Lock()
	if set, ok := h.conns[userID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, userID)
		}
	}
	h.mu.Unlock()

	c.close()
}

// Send отправляет payload всем соединениям пользователя и возвращает число
// тех, в которые запись прошла. Соединения, в которые записать не удалось,
// снимаются с регистрации.
//
// Возвращаемая ошибка — только про сериализацию payload: она общая для всех
// соединений, а отвал одного соединения — не ошибка вызывающего.
func (h *Hub) Send(userID uuid.UUID, payload any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	// Снимок под RLock, запись — без него. Иначе медленный пир держал бы
	// блокировку весь writeTimeout и подвешивал Add/Remove всего хаба.
	h.mu.RLock()
	targets := make([]*Conn, 0, len(h.conns[userID]))
	for c := range h.conns[userID] {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	sent := 0
	for _, c := range targets {
		if sendErr := c.send(body); sendErr != nil {
			h.Remove(userID, c)
			continue
		}
		sent++
	}
	return sent, nil
}

// Online возвращает число живых соединений пользователя.
//
// Нужно не только тестам: рассылка состояния питомца дешевле, если сперва
// спросить, есть ли кому его слать, — считать снимок состояния для того, кто
// офлайн, незачем.
func (h *Hub) Online(userID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[userID])
}

// Close снимает с регистрации и закрывает все соединения. После Close хаб
// снова пригоден к использованию — это упрощает тесты и не мешает остановке
// сервера, где хаб просто больше не нужен.
func (h *Hub) Close() {
	h.mu.Lock()
	old := h.conns
	h.conns = make(map[uuid.UUID]map[*Conn]struct{})
	h.mu.Unlock()

	for _, set := range old {
		for c := range set {
			c.close()
		}
	}
}
