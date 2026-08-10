package rewards

// Слой транспорта: знает про Gin и типы контракта, не знает, как считается
// eligibility или как выдаётся грант — то service.go. depguard (handler-layer)
// запрещает этому файлу импортировать pgx.

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tamagochi/internal/api"
	"tamagochi/internal/httpx"
	"tamagochi/pkg/authctx"
)

// Handler отвечает на HTTP для тега rewards.
type Handler struct {
	svc *Service
}

// NewHandler собирает обработчик.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register вешает маршруты тега rewards на переданную группу.
func (h *Handler) Register(r gin.IRoutes) {
	r.GET("/rewards", h.list)
	r.GET("/rewards/next", h.next)
	r.POST("/rewards/:rewardId/claim", h.claim)
	r.POST("/rewards/:rewardId/redeem-click", h.redeem)
}

// list отвечает на GET /rewards.
//
// Параметр status контракт объявляет необязательным (фильтр), но фильтрацию
// по нему этот срез не делает — список короткий (весь каталог), фронту
// дешевле отфильтровать самому, чем плодить на бэке параметр, который нигде
// не проверяется на допустимые значения.
func (h *Handler) list(c *gin.Context) {
	userID, ok := authctx.UserID(c.Request.Context())
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	rewardsList, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
		return
	}

	items := make([]api.Reward, 0, len(rewardsList))
	for _, r := range rewardsList {
		items = append(items, toAPIReward(r))
	}
	httpx.OK(c.Writer, c.Request, http.StatusOK, items, "Награды")
}

// next отвечает на GET /rewards/next.
func (h *Handler) next(c *gin.Context) {
	userID, ok := authctx.UserID(c.Request.Context())
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	reward, found, err := h.svc.Next(c.Request.Context(), userID)
	if err != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
		return
	}
	if !found {
		httpx.Fail(c.Writer, c.Request, http.StatusNotFound, api.NOTFOUND, "Все награды каталога уже получены")
		return
	}
	httpx.OK(c.Writer, c.Request, http.StatusOK, toAPIReward(reward), "Ближайшая награда")
}

// claim отвечает на POST /rewards/{rewardId}/claim.
func (h *Handler) claim(c *gin.Context) {
	userID, ok := authctx.UserID(c.Request.Context())
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	rewardID := c.Param("rewardId")

	// Idempotency-Key обязателен контрактом (IdempotencyKeyRequired) — тот
	// самый ключ, которым Repo.Claim отличает легитимный ретрай от второй
	// попытки получить уже выданную награду.
	raw := c.GetHeader("Idempotency-Key")
	idempotencyKey, parseErr := uuid.Parse(raw)
	if parseErr != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, "Заголовок Idempotency-Key обязателен и должен быть UUID")
		return
	}

	result, err := h.svc.Claim(c.Request.Context(), userID, rewardID, idempotencyKey)
	switch {
	case errors.Is(err, ErrUnknownReward):
		httpx.Fail(c.Writer, c.Request, http.StatusNotFound, api.NOTFOUND, "Такой награды нет")
		return
	case errors.Is(err, ErrNoPet), errors.Is(err, ErrNotEligible):
		httpx.Fail(c.Writer, c.Request, http.StatusConflict, api.REWARDNOTELIGIBLE, "Условие награды не выполнено")
		return
	case errors.Is(err, ErrAlreadyClaimed):
		// Контракт: в data — та же награда, не пустая ошибка — фронт
		// открывает тот же экран, а не показывает ошибку поверх формы.
		current, listErr := h.svc.List(c.Request.Context(), userID)
		if listErr != nil {
			httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
			return
		}
		for _, r := range current {
			if r.ID == rewardID {
				httpx.FailWithData(c.Writer, c.Request, http.StatusConflict, api.REWARDALREADYCLAIMED, "Награда уже получена другим запросом", toAPIReward(r))
				return
			}
		}
		httpx.Fail(c.Writer, c.Request, http.StatusConflict, api.REWARDALREADYCLAIMED, "Награда уже получена другим запросом")
		return
	case err != nil:
		httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
		return
	}

	httpx.OK(c.Writer, c.Request, http.StatusOK, toAPIReward(result.Reward), "Награда получена")
}

// redeem отвечает на POST /rewards/{rewardId}/redeem-click.
func (h *Handler) redeem(c *gin.Context) {
	userID, ok := authctx.UserID(c.Request.Context())
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	reward, err := h.svc.Redeem(c.Request.Context(), userID, c.Param("rewardId"))
	switch {
	case errors.Is(err, ErrUnknownReward):
		httpx.Fail(c.Writer, c.Request, http.StatusNotFound, api.NOTFOUND, "Такой награды нет")
		return
	case errors.Is(err, ErrNotClaimed):
		httpx.Fail(c.Writer, c.Request, http.StatusConflict, api.REWARDNOTCLAIMED, "Награду сначала нужно получить")
		return
	case err != nil:
		httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
		return
	}

	httpx.OK(c.Writer, c.Request, http.StatusOK, toAPIReward(reward), "Награда применена")
}

// toAPIReward переводит доменную Reward в тип контракта.
func toAPIReward(r Reward) api.Reward {
	out := api.Reward{
		Id:             r.ID,
		Tier:           api.RewardTier(r.Tier),
		Title:          r.Title,
		ConditionLabel: r.ConditionLabel,
		Status:         api.RewardStatus(r.Status),
		Progress: struct {
			Current int                    `json:"current"`
			Target  int                    `json:"target"`
			Unit    api.RewardProgressUnit `json:"unit"`
		}{
			Current: r.ProgressCurrent,
			Target:  r.ProgressTarget,
			Unit:    api.RewardProgressUnitLevel,
		},
	}
	if r.Description != "" {
		description := r.Description
		out.Description = &description
	}
	if r.CosmeticID != "" {
		cosmeticID := r.CosmeticID
		out.CosmeticId = &cosmeticID
	}
	if r.ClaimedAt != nil {
		claimedAt := *r.ClaimedAt
		out.ClaimedAt = &claimedAt
	}
	if r.UsedAt != nil {
		usedAt := *r.UsedAt
		out.UsedAt = &usedAt
	}
	return out
}
