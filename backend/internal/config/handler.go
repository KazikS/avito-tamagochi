package config

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"tamagochi/internal/api"
	"tamagochi/internal/httpx"
)

// Handler отдаёт справочник экономики.
//
// Слой транспорта: знает про Gin и про типы контракта, не знает, как считается
// кривая — это service.go. Правило проверяется depguard'ом (handler-layer),
// а не ревью.
type Handler struct {
	curve          Curve
	dailyCareXPCap int
}

// NewHandler собирает обработчик и отказывается собираться на бессмысленных
// константах.
//
// Проверка здесь, а не в обработчике запроса: кривая приходит из констант
// сборки, и ошибка в ней — это не «клиент прислал ерунду», а неработающий
// сервис. Пусть падает на старте, а не на первом запросе пользователя.
func NewHandler(curve Curve, dailyCareXPCap int) (*Handler, error) {
	if err := curve.Validate(); err != nil {
		return nil, err
	}
	if dailyCareXPCap < 0 {
		return nil, fmt.Errorf("config: суточный кап опыта %d, ожидалось не меньше нуля", dailyCareXPCap)
	}
	return &Handler{curve: curve, dailyCareXPCap: dailyCareXPCap}, nil
}

// Register вешает маршруты тега config на переданную группу.
//
// Путь относительный: группу с префиксом (/api/v1 — из блока servers
// контракта) собирает cmd/, а не пакет фичи.
func (h *Handler) Register(r gin.IRoutes) {
	r.GET("/config", h.get)
}

// get отвечает на GET /config.
func (h *Handler) get(c *gin.Context) {
	// Локальные копии: в контракте все поля Config — указатели, а брать адрес
	// поля структуры-получателя значило бы отдать наружу ссылку на состояние
	// обработчика, общее для всех запросов.
	base := h.curve.Base
	roundTo := h.curve.RoundTo
	maxLevel := h.curve.MaxLevel
	dailyCap := h.dailyCareXPCap

	// float32 — так это поле объявлено в контракте (number без format).
	// Значения кривой задаются с двумя знаками после точки, float32 их
	// представляет точно, потери на этом преобразовании нет.
	factor := float32(h.curve.Factor)

	// Остальные поля Config (stats, actions, moods, streakTiers, milestones)
	// намеренно пустые: это экономика и концепт питомца, а они в AGENTS.md
	// стоп-вопросы — решает команда, не агент. Все поля Config необязательные,
	// поэтому частичный ответ валиден по контракту.
	httpx.OK(c.Writer, c.Request, http.StatusOK, api.Config{
		DailyCareXpCap: &dailyCap,
		LevelCurve: &api.LevelCurve{
			Base:     &base,
			Factor:   &factor,
			RoundTo:  &roundTo,
			MaxLevel: &maxLevel,
		},
	}, "Справочник экономики")
}
