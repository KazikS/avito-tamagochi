package main

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tamagochi/pkg/authctx"
)

// demoUserID — фиксированный пользователь demo-стенда.
//
// Реальной авторизации ещё нет (feat/auth не смержена, схема users не
// решена — docs/RECONCILIATION.md). Эта заглушка существует, чтобы
// internal/pet можно было проверить по HTTP уже сейчас, и удаляется целиком
// в тот день, когда приезжает вход по login+password.
var demoUserID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// demoIdentityHeader — заголовок, которым можно переключиться на другого
// demo-пользователя. Нужен, чтобы вручную проверить «второй питомец другому
// пользователю не мешает первому» через curl, не поднимая настоящий вход.
const demoIdentityHeader = "X-Demo-User-Id"

// withDemoIdentity кладёт идентификатор пользователя в контекст запроса.
//
// Смонтирован ТОЛЬКО когда APP_ENV=demo (см. вызывающий код в newRouter):
// без явного флага окружения любой запрос к серверу действовал бы от имени
// demoUserID без единой проверки пароля — то самое «незакрытый debug-эндпоинт»
// из класса дефектов, который в этом проекте уже находили руками.
func withDemoIdentity() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := demoUserID
		if raw := c.GetHeader(demoIdentityHeader); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			id = parsed
		}
		ctx := authctx.WithUserID(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// demoModeEnabled сообщает, разрешено ли монтировать demo-заглушки.
//
// Читает окружение здесь и один раз при сборке роутера, а не внутри
// withDemoIdentity на каждый запрос: значение не меняется, пока процесс жив,
// а решение «доступен ли вообще этот путь» должно приниматься один раз на
// старте, а не на каждый HTTP-запрос.
func demoModeEnabled() bool {
	return os.Getenv("APP_ENV") == "demo"
}
