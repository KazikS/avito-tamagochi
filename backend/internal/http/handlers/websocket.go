package handlers

import (
	"net/http"
	"tamagochi/backend/internal/usecase"
	ws "tamagochi/backend/internal/websocket"
	"tamagochi/backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type WebSocketHandler struct {
	uc usecase.PetService
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// На хакатоне отключаем проверку CORS, чтобы фронтенд на Vite
	// мог без проблем подключаться с localhost
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (wsh *WebSocketHandler) ServeWS(c *gin.Context) {
	// 3. Выполняем апгрейд: передаем ResponseWriter и Request
	// Метод Upgrade берет HTTP-запрос и превращает его в постоянное WS-соединение
	log, ok := logger.GetLoggerFromCtx(c.Request.Context())
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "INTERNAL_ERROR",
			"message": "Unexpected error occurred",
		})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error(c.Request.Context(), err, "Ошибка апгрейда до WebSocket:")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "INTERNAL_ERROR",
			"message": err.Error(),
		})
		return
	}

	userID, ok := c.Request.Context().Value(logger.RequestIDKey).(string)
	if !ok {
		log.Error(c.Request.Context(), nil, "no userID in context")
	}
	ws.Manager.AddClient(userID, conn)

	defer func() {
		ws.Manager.RemoveClient(userID)
		conn.Close()
		log.Info(c.Request.Context(), "Пользователь отключился от WS", zap.String("id", userID))
	}()

	log.Info(c.Request.Context(), "Фронтенд успешно подключился по WebSocket!")

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
