package wsh

import (
	"time"

	"github.com/gorilla/websocket"
)

// KeepAlive держит соединение живым и обнаруживает мёртвых пиров, пока не
// вернётся ошибка чтения (пир отключился, дедлайн истёк, разрыв сети).
//
// Дедлайн чтения — намеренно НЕ параметр вызывающего пакета и НЕ идёт через
// pkg/clock: это livenесс TCP-соединения, а не доменное или бизнес-время.
// Показательный урок этого файла: он появился ПОСЛЕ того, как первая версия
// serveWS в internal/pet/ws.go считала дедлайн через тот же clock.Clock, что
// и Service — чтобы угодить forbidigo («time.Now() только в pkg/ и cmd/»).
// В тестах clock.Fixed стоит на одной точке в 2026 году; SetReadDeadline на
// РЕАЛЬНОМ сокете получил эту точку как абсолютное время, и все три теста
// WS немедленно ловили "close 1006 (abnormal closure): unexpected EOF" —
// сокет считал себя просроченным с первой же операции. Правило «время —
// параметр» верно для домена и неверно для сетевого уровня одного и того же
// приложения: перепутать их — не то же самое, что молча подавить линтер, но
// стоило столько же отладки. pkg/wsh — разрешённое место для time.Now()
// именно потому, что здесь оно про сеть, а не про решение.
func KeepAlive(ws *websocket.Conn, interval time.Duration) {
	_ = ws.SetReadDeadline(time.Now().Add(interval))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(interval))
	})

	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		_ = ws.SetReadDeadline(time.Now().Add(interval))
	}
}
