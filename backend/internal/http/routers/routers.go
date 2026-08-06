// Package routers регистрирует все хендлеры
package routers

import (
	"tamagochi/backend/internal/http/handlers"

	"github.com/gin-gonic/gin"
)

func RegisterRoutes(e *gin.Engine, h handlers.Handlers) {
	e.GET("/ws", h.Ws.ServeWS)
	api := e.Group("/api")
	
	registerPetRouters(api, h.Pet)
}


func registerPetRouters(api *gin.RouterGroup, h handlers.PetHandler) {
	pet := api.Group("/pet")
	{
		pet.GET("/", h.GetPetInfo)
		pet.POST("/action")
	}
}
