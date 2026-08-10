package social

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapitypes "github.com/oapi-codegen/runtime/types"

	"tamagochi/internal/advisor"
	"tamagochi/internal/api"
	"tamagochi/internal/httpx"
	"tamagochi/internal/pet"
	"tamagochi/internal/rewards"
	"tamagochi/pkg/authctx"
)

// Слой транспорта: знает про Gin и типы контракта, не знает, как считается
// ранг или окно недели — то service.go. depguard (handler-layer) запрещает
// этому файлу импортировать pgx.
//
// aiNote — единственное поле сводки, для которого этому файлу нужны ДВЕ
// чужие фичи разом (rewards.Service.Next, advisor.Provider): Summary
// (service.go) остаётся чистой функцией без сети, а композиция нужного
// клиенту ответа — то, для чего и существует handler.go. Ничего из этого
// не течёт обратно в Service.

// Handler отвечает на HTTP для тега social.
type Handler struct {
	svc     *Service
	rewards *rewards.Service
	advisor advisor.Provider
}

// NewHandler собирает обработчик. advisorProvider может быть nil — тогда
// aiNote в ответе не заполняется вообще (честное отсутствие, тот же приём,
// что уже был до этой фичи, см. toAPISummary).
func NewHandler(svc *Service, rewardsSvc *rewards.Service, advisorProvider advisor.Provider) *Handler {
	return &Handler{svc: svc, rewards: rewardsSvc, advisor: advisorProvider}
}

// Register вешает маршруты тега social на переданную группу.
func (h *Handler) Register(r gin.IRoutes) {
	r.GET("/leaderboard", h.leaderboard)
	r.GET("/summary/daily", h.summaryDaily)
	r.POST("/summary/daily/seen", h.summaryDailySeen)
}

// leaderboard отвечает на GET /leaderboard.
func (h *Handler) leaderboard(c *gin.Context) {
	userID, ok := authctx.UserID(c.Request.Context())
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	// Возвращаемое значение не нужно: единственный успешный исход ParseScope —
	// ScopeTop, второй раз проверять уже нечего.
	if _, err := ParseScope(c.Query("scope")); err != nil {
		msg := "scope должен быть одним из: top, league, friends"
		if errors.Is(err, ErrUnsupportedScope) {
			msg = "Этот scope лидерборда пока не реализован: доступен только top"
		}
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, msg)
		return
	}

	limit := NormalizeLimit(parseLimit(c.Query("limit")))

	page, err := h.svc.List(c.Request.Context(), userID, c.Query("cursor"), limit)
	switch {
	case errors.Is(err, ErrBadCursor):
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, "Некорректный cursor")
		return
	case err != nil:
		httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
		return
	}

	httpx.OK(c.Writer, c.Request, http.StatusOK, toAPIPage(page), "Лидерборд")
}

// parseLimit разбирает limit из query. Некорректное значение (не число,
// отрицательное) читается как «лимит не задан» — NormalizeLimit подставит
// дефолт; отдельная 422 ради опечатки в необязательном параметре было бы
// суровее, чем того просит контракт.
func parseLimit(raw string) *int {
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return nil
	}
	return &n
}

// toAPIEntry переводит доменную Entry в тип контракта.
func toAPIEntry(e Entry) api.LeaderboardEntry {
	userID := e.UserID.String()
	level := e.Level
	nickname := e.Nickname
	presetID := api.PresetId(e.PresetID)
	rank := e.Rank
	streak := e.Streak
	weeklyXP := e.WeeklyXP
	isMe := e.IsMe
	return api.LeaderboardEntry{
		UserId:   &userID,
		Nickname: &nickname,
		PresetId: &presetID,
		Level:    &level,
		Streak:   &streak,
		WeeklyXp: &weeklyXP,
		Rank:     &rank,
		IsMe:     &isMe,
	}
}

// toAPIPage переводит доменную Page в тип контракта.
//
// League намеренно nil: поле относится к scope=league, которого этот срез
// не реализует (см. докстринг пакета). Пустое поле в JSON честнее выдуманных
// данных о лиге, которой нет.
func toAPIPage(p Page) api.LeaderboardPage {
	items := make([]api.LeaderboardEntry, 0, len(p.Items))
	for _, e := range p.Items {
		items = append(items, toAPIEntry(e))
	}

	scope := api.LeaderboardPageScope(ScopeTop)
	out := api.LeaderboardPage{
		Items: &items,
		Scope: &scope,
	}
	if p.NextCursor != "" {
		out.NextCursor = &p.NextCursor
	}
	if p.Me != nil {
		me := toAPIEntry(*p.Me)
		out.Me = &me
	}
	return out
}

// --- Сводка дня ----------------------------------------------------------

// summaryDaily отвечает на GET /summary/daily.
func (h *Handler) summaryDaily(c *gin.Context) {
	userID, ok := authctx.UserID(c.Request.Context())
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	date, err := parseSummaryDate(c.Query("date"))
	if err != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, "date должен быть в формате YYYY-MM-DD")
		return
	}

	summary, err := h.svc.Summary(c.Request.Context(), userID, date)
	if err != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
		return
	}

	out := toAPISummary(summary)
	if note, noteOK := h.aiNote(c.Request.Context(), userID, summary); noteOK {
		out.AiNote = &note
	}

	httpx.OK(c.Writer, c.Request, http.StatusOK, out, "Сводка дня")
}

// aiNote считает advisor.Situation поверх Summary и rewards.Service.Next и
// зовёт h.advisor. Любой сбой (advisor не сконфигурирован, ошибка модели,
// таймаут) — просто ok=false: aiNote остаётся отсутствующим полем, ровно
// как до этой фичи, а не 500 всей сводке дня из-за необязательного текста.
func (h *Handler) aiNote(ctx context.Context, userID uuid.UUID, s Summary) (string, bool) {
	if h.advisor == nil || !s.HasPetSnapshot {
		return "", false
	}

	situation := advisor.Situation{
		PetName:          "Ави",
		Level:            s.LevelAfter,
		LeveledUp:        s.LevelAfter > s.LevelBefore,
		AvailableActions: availableActions(s.Actions),
	}
	situation.LowestStat, situation.LowestStatValue = lowestStat(s.StatsAfter)

	if h.rewards != nil {
		if reward, found, err := h.rewards.Next(ctx, userID); err == nil && found {
			situation.NextRewardTitle = reward.Title
			situation.NextRewardLevelsAway = reward.ProgressTarget - reward.ProgressCurrent
		}
	}

	advice, err := h.advisor.Advise(ctx, situation)
	if err != nil || advice.Note == "" {
		return "", false
	}
	return advice.Note, true
}

// lowestStat — какой из четырёх показателей питомца сейчас ниже остальных.
// Не «просел за сутки» (истории по показателям нет) — то, что нуждается в
// заботе прямо сейчас, по последнему известному снимку.
func lowestStat(s pet.Stats) (name string, value int) {
	type kv struct {
		name  string
		value int
	}
	stats := []kv{
		{"hunger", s.Hunger},
		{"joy", s.Joy},
		{"clean", s.Clean},
		{"energy", s.Energy},
	}
	lowest := stats[0]
	for _, x := range stats[1:] {
		if x.value < lowest.value {
			lowest = x
		}
	}
	return lowest.name, lowest.value
}

// availableActions — действия ухода, у которых сегодня остался лимит, в
// порядке pet.ActionKinds. wake сюда не попадает: это не совет «сделай
// что-нибудь», а контекстное действие, доступное только спящему питомцу.
func availableActions(actions map[pet.ActionKind]pet.Availability) []string {
	out := make([]string, 0, len(actions))
	for _, kind := range pet.ActionKinds {
		if kind == pet.ActionWake {
			continue
		}
		if a, ok := actions[kind]; ok && a.Remaining > 0 {
			out = append(out, string(kind))
		}
	}
	return out
}

// summaryDailySeen отвечает на POST /summary/daily/seen.
func (h *Handler) summaryDailySeen(c *gin.Context) {
	userID, ok := authctx.UserID(c.Request.Context())
	if !ok {
		httpx.Fail(c.Writer, c.Request, http.StatusUnauthorized, api.UNAUTHORIZED, "Нужен вход")
		return
	}

	// api.PostSummaryDailySeenJSONBody — сгенерированный тип тела запроса:
	// его openapi_types.Date уже умеет разбирать "2026-08-09" через
	// json.Unmarshal, переписывать этот разбор вручную незачем (AGENTS.md —
	// once codegen wired, generated types win). Но сам сгенерированный тип не
	// несёт тега binding:"required" (oapi-codegen его не проставляет для
	// голых типов), поэтому Gin молча пропускает отсутствующее поле как
	// нулевое время — IsZero проверяется отдельно, руками, иначе отсутствие
	// date в теле тихо запишет "0001-01-01" вместо честной 422.
	var body api.PostSummaryDailySeenJSONBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Date.IsZero() {
		httpx.Fail(c.Writer, c.Request, http.StatusUnprocessableEntity, api.VALIDATIONERROR, "date обязателен и должен быть в формате YYYY-MM-DD")
		return
	}

	if err := h.svc.MarkSeen(c.Request.Context(), userID, body.Date.Time); err != nil {
		httpx.Fail(c.Writer, c.Request, http.StatusInternalServerError, api.INTERNALERROR, "Внутренняя ошибка сервера")
		return
	}

	c.Status(http.StatusNoContent)
}

// parseSummaryDate разбирает query-параметр date контракта. Пустая строка —
// не ошибка, это «дата не задана»: Service.Summary сам подставит сегодня по
// своим часам. Bind — метод самого openapi_types.Date, написанный ровно под
// разбор скалярного query-параметра (см. docstring в rutime/types/date.go).
func parseSummaryDate(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	var d openapitypes.Date
	if err := d.Bind(raw); err != nil {
		return nil, err
	}
	return &d.Time, nil
}

// toAPISummary переводит доменную Summary в тип контракта.
//
// Streak/Tomorrow остаются nil намеренно: система стриков не построена (см.
// докстринг Summary в service.go) — контракт помечает оба необязательными
// именно для случаев вроде этого, честное отсутствие поля лучше выдуманного
// значения под его именем. AiNote сюда не входит — его выставляет
// summaryDaily поверх результата этой функции (см. h.aiNote), не сама эта
// функция: чистый маппинг Summary→DailySummary не должен звонить в advisor.
func toAPISummary(s Summary) api.DailySummary {
	date := openapitypes.Date{Time: s.Date}
	shouldShow := s.ShouldShow
	xpTotal := s.XPTotal
	multiplier := float32(s.Multiplier)

	breakdown := make([]struct {
		Count *int    `json:"count,omitempty"`
		Key   *string `json:"key,omitempty"`
		Label *string `json:"label,omitempty"`
		Xp    *int    `json:"xp,omitempty"`
	}, 0, len(s.Breakdown))
	for _, b := range s.Breakdown {
		count, xp, key, label := b.Count, b.XP, b.Key, b.Label
		breakdown = append(breakdown, struct {
			Count *int    `json:"count,omitempty"`
			Key   *string `json:"key,omitempty"`
			Label *string `json:"label,omitempty"`
			Xp    *int    `json:"xp,omitempty"`
		}{Count: &count, Key: &key, Label: &label, Xp: &xp})
	}

	out := api.DailySummary{
		Date:       &date,
		ShouldShow: &shouldShow,
		XpTotal:    &xpTotal,
		Multiplier: &multiplier,
		Breakdown:  &breakdown,
	}

	if s.HasPetSnapshot {
		levelBefore, levelAfter := s.LevelBefore, s.LevelAfter
		moodAfter := api.PetMood(s.MoodAfter)
		statsAfter := api.Stats{Hunger: s.StatsAfter.Hunger, Joy: s.StatsAfter.Joy, Clean: s.StatsAfter.Clean, Energy: s.StatsAfter.Energy}
		out.Pet = &struct {
			LevelAfter  *int `json:"levelAfter,omitempty"`
			LevelBefore *int `json:"levelBefore,omitempty"`

			// MoodAfter Производная от МИНИМАЛЬНОГО показателя. Клиент её не вычисляет
			MoodAfter *api.PetMood `json:"moodAfter,omitempty"`

			// MoodBefore Производная от МИНИМАЛЬНОГО показателя. Клиент её не вычисляет
			MoodBefore *api.PetMood `json:"moodBefore,omitempty"`

			// StatsAfter Целые 0..100. Потолок жёсткий: 82 + 30 = 100, остаток сгорает
			StatsAfter *api.Stats `json:"statsAfter,omitempty"`
		}{
			LevelBefore: &levelBefore,
			LevelAfter:  &levelAfter,
			MoodAfter:   &moodAfter,
			StatsAfter:  &statsAfter,
		}
	}

	return out
}
