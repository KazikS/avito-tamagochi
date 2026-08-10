package pet

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tamagochi/internal/api"
	"tamagochi/internal/httpx"
	"tamagochi/pkg/authctx"
	"tamagochi/pkg/clock"
	"tamagochi/pkg/wsh"
)

// Слой транспорта: знает про Gin и про типы контракта, не знает, как считается
// распад, настроение или опыт — это service.go. depguard (handler-layer)
// запрещает этому файлу импортировать pgx: обращение к базе только через
// Service, который вызывает Repo.

// Handler отвечает на HTTP для тега pet.
type Handler struct {
	svc *Service
	hub *wsh.Hub
	clk clock.Clock
	bc  *Broadcaster
}

// NewHandler собирает обработчик. hub может быть nil — тогда WS не работает,
// а HTTP-часть (create/get/act) остаётся полностью рабочей: полезно в тестах,
// которым события реального времени не нужны.
func NewHandler(svc *Service, hub *wsh.Hub, clk clock.Clock) *Handler {
	h := &Handler{svc: svc, hub: hub, clk: clk}
	if hub != nil {
		h.bc = NewBroadcaster(hub, clk)
	}
	return h
}

// Register вешает REST-маршруты тега pet на переданную группу (/api/v1).
func (h *Handler) Register(r gin.IRoutes) {
	r.POST("/pets", h.create)
	r.GET("/pet", h.get)
	r.POST("/pet/actions", h.act)
}

// RegisterWS вешает /ws — ВНЕ apiPrefix, ровно по URL из контракта
// (docs/openapi.json → x-websocket.url: `wss://.../ws?token=...`, без
// префикса /api/v1). Отдельный метод, а не строка в Register: вызывающий
// (cmd/wire.go) обязан отдать её другой группе маршрутов, и разные сигнатуры
// делают перепутанный вызов ошибкой компиляции, а не тихим багом маршрутизации.
func (h *Handler) RegisterWS(r gin.IRoutes) {
	r.GET("/ws", h.serveWS)
}

// actor достаёт пользователя из контекста запроса.
//
// Пока нет авторизации, контекст наполняет demoIdentity (см. demo.go) — она
// монтируется только под APP_ENV=demo. Без неё authctx.UserID возвращает
// false и обработчик отвечает 401, а не действует от чьего-то чужого имени.
func actor(c *gin.Context) (Actor, bool) {
	id, ok := authctx.UserID(c.Request.Context())
	if !ok {
		return Actor{}, false
	}
	// Таймзона аккаунта приедет вместе с профилем; до авторизации это ровно то
	// поле, которого у demo-пользователя нет, поэтому UTC — не решение по
	// таймзоне, а единственное, что можно подставить без профиля.
	return Actor{UserID: id, Location: nil}, true
}

// create отвечает на POST /pets.
func (h *Handler) create(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	var body api.PostPetsJSONBody
	if err := c.ShouldBindJSON(&body); err != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, "Некорректное тело запроса")
		return
	}

	name := "Ави"
	if body.Name != nil && *body.Name != "" {
		name = *body.Name
	}

	view, err := h.svc.Create(c.Request.Context(), a, string(body.PresetId), name)
	switch {
	case errors.Is(err, ErrPetExists):
		httpx.Fail(c.Writer, c.Request, http.StatusConflict, api.PETALREADYEXISTS, "Питомец уже создан")
		return
	case err != nil:
		internalError(c, err)
		return
	}

	httpx.OK(c.Writer, c.Request, http.StatusOK, toAPIPet(view), "Питомец создан")
}

// get отвечает на GET /pet.
func (h *Handler) get(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	view, err := h.svc.Get(c.Request.Context(), a)
	switch {
	case errors.Is(err, ErrNoPet):
		httpx.Fail(c.Writer, c.Request, http.StatusNotFound, api.NOTFOUND, "Питомца ещё нет")
		return
	case err != nil:
		internalError(c, err)
		return
	}

	httpx.OK(c.Writer, c.Request, http.StatusOK, toAPIPet(view), "Состояние питомца")
}

// act отвечает на POST /pet/actions.
func (h *Handler) act(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	var body api.PostPetActionsJSONBody
	if err := c.ShouldBindJSON(&body); err != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, "Некорректное тело запроса")
		return
	}
	actionID, err := uuid.Parse(body.ActionId.String())
	if err != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, "actionId должен быть UUID")
		return
	}

	res, replayed, err := h.svc.Act(c.Request.Context(), a, actionID, ActionKind(body.Kind))
	switch {
	case errors.Is(err, ErrNoPet):
		httpx.Fail(c.Writer, c.Request, http.StatusNotFound, api.NOTFOUND, "Питомца ещё нет")
		return
	case errors.Is(err, ErrUnknownAction):
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, "Неизвестное действие")
		return
	case errors.Is(err, ErrDailyLimit):
		httpx.Fail(c.Writer, c.Request, http.StatusTooManyRequests, api.ACTIONLIMITREACHED, "Суточный лимит действия исчерпан")
		return
	case errors.Is(err, ErrAlreadyAsleep), errors.Is(err, ErrNotAsleep), errors.Is(err, ErrAsleep):
		httpx.Fail(c.Writer, c.Request, http.StatusConflict, api.PETISSLEEPING, "Питомец спит")
		return
	case err != nil:
		internalError(c, err)
		return
	}

	// Пуш — после того, как HTTP-ответ решён, но до его записи: неудача пуша
	// (никто не слушает, соединение мертво) не должна влиять на HTTP-ответ,
	// а лишний поход в сеть — не повод задержать его дальше необходимого.
	if h.bc != nil {
		h.bc.PushAction(a.UserID, ActionKind(body.Kind), res, replayed)
	}

	httpx.OK(c.Writer, c.Request, http.StatusOK, toAPIActionResult(res), "Питомец обновлён")
}

// internalError отвечает 500. Ошибка не разбирается на код контракта: сюда
// попадают только состояния, которые не должны случаться при исправном коде
// (сбой базы, испорченные данные), и им нечего сказать клиенту, кроме честного
// «что-то сломалось».
func internalError(c *gin.Context, err error) {
	// Подробности намеренно не уезжают в тело ответа: тело — для пользователя.
	// Логгера в проекте пока нет ни в одном handler.go, поэтому err здесь
	// именно гасится, а не пишется в лог, — не «уже логируется». Появится
	// логгер — гасить перестанем; пока честнее сказать, что следа нет.
	_ = err
	httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
}

// toAPIStats переводит доменные Stats в тип контракта.
//
// Отдельная функция, а не инлайн в каждом месте: ту же конверсию использует
// и toAPIPet (REST), и ws.go (payload события pet.stats) — без неё поля
// расходятся молча, как уже случилось один раз (см. докстринг StatsPayload).
func toAPIStats(s Stats) api.Stats {
	return api.Stats{Hunger: s.Hunger, Joy: s.Joy, Clean: s.Clean, Energy: s.Energy}
}

// toAPIPet переводит доменный View в тип контракта.
//
// Отдельная функция, а не встраивание в service.go: маппинг в JSON-теги
// контракта — забота транспорта, а не домена. service.go не импортирует
// internal/api ровно по этой причине (depguard, service-layer).
func toAPIPet(v View) api.Pet {
	actions := make(map[string]api.ActionState, len(v.Actions))
	for kind, avail := range v.Actions {
		actions[string(kind)] = api.ActionState{
			Remaining: avail.Remaining,
			XpCapped:  avail.XPCapped,
		}
	}

	return api.Pet{
		Id:             v.ID.String(),
		PresetId:       api.PresetId(v.PresetID),
		Name:           v.Name,
		Level:          v.Level,
		Xp:             v.XP,
		XpToNext:       v.XPToNext,
		TotalXp:        v.TotalXP,
		Stage:          api.PetStage(v.Stage),
		StageLabel:     &v.StageLabel,
		Stats:          toAPIStats(v.Stats),
		Mood:           api.PetMood(v.Mood),
		MoodMultiplier: float32(v.MoodMultiplier),
		Actions:        actions,
		UpdatedAt:      v.UpdatedAt,
	}
}

// toAPIActionResult переводит доменный ActResult в тип контракта.
func toAPIActionResult(r ActResult) api.ActionResult {
	careXPToday := r.CareXPToday
	capReached := r.CareXPCapReached
	return api.ActionResult{
		Pet:              toAPIPet(r.Pet),
		XpGained:         r.XPGained,
		StatCapped:       r.StatCapped,
		LeveledUp:        r.LeveledUp,
		CareXpToday:      &careXPToday,
		CareXpCapReached: &capReached,
	}
}
