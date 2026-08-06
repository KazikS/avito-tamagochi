// Package websocket содержит инструменты для работы с websocket-соединением
package websocket

import (
	"sync"

	"github.com/gorilla/websocket"
)

// ClientManager -  хранит все активные WS-соединения
type ClientManager struct {
	sync.RWMutex
	Clients map[string]*websocket.Conn // ключ — userID (UUID в виде строки)
}

// Manager - Глобальный экземпляр менеджера
var Manager = &ClientManager{
	Clients: make(map[string]*websocket.Conn),
}

// AddClient - Метод для безопасного добавления клиента
func (m *ClientManager) AddClient(userID string, conn *websocket.Conn) {
	m.Lock()
	defer m.Unlock()
	m.Clients[userID] = conn
}

// RemoveClient - Метод для безопасного удаления клиента (когда юзер закрыл вкладку)
func (m *ClientManager) RemoveClient(userID string) {
	conn := m.Clients[userID]
	defer conn.Close()

	m.Lock()
	defer m.Unlock()
	delete(m.Clients, userID)
}
