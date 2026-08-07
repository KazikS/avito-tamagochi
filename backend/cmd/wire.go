package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"tamagochi/internal/api"
	"tamagochi/internal/config"
	"tamagochi/internal/httpx"
)

// apiPrefix — префикс всех эндпоинтов контракта. Взят из блока servers в
// docs/openapi.json (http://localhost:8080/api/v1), а не придуман здесь.
const apiPrefix = "/api/v1"

// newRouter собирает роутер целиком.
//
// Вынесено из main отдельным файлом, а не дописано в main.go, по одной
// конкретной причине: main.go переписывает и feat/pet-service, и всякий, кто
// добавляет свой пакет, — это самый конфликтный файл бэкенда. Пока сборка
// живёт здесь, чужой мерж трогает три строки в main.go, а не весь wiring.
func newRouter() (*gin.Engine, error) {
	// ReleaseMode: в отладочном Gin печатает в stdout список маршрутов и
	// предупреждение о режиме на каждом старте, включая каждый запуск тестов.
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()

	// Recovery, но не Logger: логировать каждый запрос через принтер Gin'а
	// незачем, а паника в обработчике не должна ронять процесс целиком.
	r.Use(gin.Recovery())

	// Без этого Gin отвечает 404 на существующий путь с другим методом, и
	// клиент контракта не отличит «нет такого ресурса» от «нельзя так».
	r.HandleMethodNotAllowed = true

	// Ответы на несуществующий маршрут — в том же конверте, что и всё
	// остальное. Иначе клиент, который умеет разбирать { data, meta },
	// на первой же опечатке в URL получает текст «404 page not found».
	r.NoRoute(func(c *gin.Context) {
		httpx.Fail(c.Writer, c.Request, http.StatusNotFound, api.NOTFOUND, "Ресурс не найден")
	})
	// Отдельного кода для 405 в перечислении ErrorCode нет; NOT_FOUND здесь
	// читается как «такого маршрута с таким методом нет», что и произошло.
	r.NoMethod(func(c *gin.Context) {
		httpx.Fail(c.Writer, c.Request, http.StatusMethodNotAllowed, api.NOTFOUND, "Метод не поддерживается")
	})

	// /healthz намеренно вне apiPrefix и вне конверта: его нет в контракте,
	// это служебная проверка для docker-compose и балансировщика, а не
	// эндпоинт для фронта.
	r.GET("/healthz", healthz)

	v1 := r.Group(apiPrefix)

	cfg, err := config.NewHandler(config.DefaultCurve, config.DefaultDailyCareXPCap)
	if err != nil {
		return nil, fmt.Errorf("сборка обработчика config: %w", err)
	}
	cfg.Register(v1)

	return r, nil
}

// healthz отвечает на служебную проверку живости.
func healthz(c *gin.Context) {
	c.Data(http.StatusOK, "application/json", []byte(`{"status":"ok"}`))
}
