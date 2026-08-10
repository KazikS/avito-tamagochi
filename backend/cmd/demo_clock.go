package main

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"tamagochi/pkg/clock"
)

// registerDemoClockDebug монтирует служебный сдвиг часов демо-стенда —
// docs/DECISIONS.md → «не решение, а несделанная работа»: жюри смотрит демо
// десять минут и не видит, как распадаются показатели, наступает новый день
// для сводки или сдвигается недельное окно лидерборда. Сдвиг дёшев ровно
// потому, что время в домене — параметр (pkg/clock), а не time.Now() внутри.
//
// Путь и поле тела (advanceHours) — те же, что уже вызывает `make clock` в
// корневом Makefile: тот таргет был написан раньше этого файла как
// заглушка-контракт для будущей реализации, реализация подстраивается под
// него, а не наоборот.
//
// Эндпоинты вне apiPrefix и вне docs/openapi.json — это пульт демо-стенда,
// не часть контракта, ровно как /healthz.
//
// Вызывается только когда демо-часы вообще существуют (demoModeEnabled() в
// newRouter) — без включённого демо-режима двигать нечего: в проде часы
// настоящие (clock.Real) и не размонтированы ни в какой Fixed.
func registerDemoClockDebug(r gin.IRoutes, clk *clock.Fixed) {
	r.GET("/debug/clock", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"now": clk.Now().Format(time.RFC3339)})
	})

	r.POST("/debug/clock/advance", requireDebugToken(), func(c *gin.Context) {
		var body struct {
			// AdvanceHours — на сколько часов вперёд сдвинуть демо-часы; дробные
			// значения допустимы (0.5 = на полчаса). Отрицательное значение
			// двигает назад (Fixed.Advance это умеет) — полезно откатить демо
			// к исходной точке между показами.
			AdvanceHours float64 `json:"advanceHours"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		clk.Advance(time.Duration(body.AdvanceHours * float64(time.Hour)))
		c.JSON(http.StatusOK, gin.H{"now": clk.Now().Format(time.RFC3339)})
	})
}

// requireDebugToken запирает мутирующий сдвиг часов токеном из переменной
// окружения DEBUG_TOKEN, если она задана. Локально и на закрытом демо-стенде
// переменную можно не задавать — поведение не меняется. На публичном URL
// (README.md → «Деплой») задать обязательно: без токена сдвинуть общие
// демо-часы (а значит и то, что видит demo-пользователь у всех посетителей
// одновременно) может кто угодно, кто найдёт путь — это не приватный стенд.
//
// GET /debug/clock токеном не закрыт: он только читает текущее показание,
// закрывать нечего.
func requireDebugToken() gin.HandlerFunc {
	token := os.Getenv("DEBUG_TOKEN")
	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}
		if c.GetHeader("X-Debug-Token") != token {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}
