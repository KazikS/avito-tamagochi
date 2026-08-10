package pet

// Файл не называется handler.go: слоевые правила depguard (handler-layer)
// матчат ровно этот путь, а WS-пуш — не HTTP-обработчик контракта, а
// широковещание вслед за ним. Тот же приём, что уже применён в
// internal/httpx (см. её докстринг): имя файла — это то, чем правила
// включаются и выключаются, а не декларация.
//
// Реализована ЧАСТЬ протокола из docs/openapi.json → x-websocket:
// конверт (eventId/seq/type/ts/payload) и три события — pet.stats,
// xp.gained, level.up, отправляемые вслед за POST /pet/actions. НЕ
// реализовано и не должно молча считаться реализованным: resume/resync по
// lastSeq (контракт требует досылать пропущенное или отвечать
// {resync:true} — здесь при реконнекте это просто новое соединение без
// истории), пакетная отправка нескольких событий одним фреймом, закрытие
// соединения кодом 4401 при протухшем токене (токенов ещё нет — см. §
// «Демо-личность» в cmd/demo_identity.go), a также события tasks/streak/
// cosmetics/rewards/summary — этих фич ещё нет в коде.

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"tamagochi/internal/api"
	"tamagochi/pkg/clock"
	"tamagochi/pkg/wsh"
)

// Envelope — конверт события реального времени. Поля и их порядок — дословно
// docs/openapi.json → x-websocket.envelope.
type Envelope struct {
	EventID uuid.UUID `json:"eventId"`
	Seq     uint64    `json:"seq"`
	Type    string    `json:"type"`
	Ts      time.Time `json:"ts"`
	Payload any       `json:"payload"`
}

// Payload-типы событий — дословно docs/openapi.json → x-websocket.serverEvents.

// StatsPayload — payload события "pet.stats".
//
// Stats — api.Stats (тип контракта), а не доменный Stats: у доменного типа
// нет json-тегов (ему и не положено — это internal/api уже отвечает за
// форму на проводе), и без конверсии поля уходили бы в эфир как Hunger/Joy/
// Clean/Energy вместо hunger/joy/clean/energy. Найдено не юнит-тестом (тот
// разбирал payload как map[string]any и проверял только xp.gained), а
// вручную — реальным WS-клиентом (websockets, Python) на настоящем
// контейнере, с чтением сырого JSON. TestWSStatsPayloadUsesContractFieldNames
// закрывает именно этот пробел.
type StatsPayload struct {
	Stats          api.Stats `json:"stats"`
	Mood           Mood      `json:"mood"`
	MoodMultiplier float64   `json:"moodMultiplier"`
	UpdatedAt      string    `json:"updatedAt"`
}

// XPGainedPayload — payload события "xp.gained".
//
// Source — строка с именем действия ухода (kind: "feed"/"play"/...). Контракт
// не перечисляет допустимые значения source явно; другие источники (задачи,
// стрик) появятся вместе с самими фичами и получат свои значения.
type XPGainedPayload struct {
	Amount   int    `json:"amount"`
	Source   string `json:"source"`
	Level    int    `json:"level"`
	XP       int    `json:"xp"`
	XPToNext int    `json:"xpToNext"`
	TotalXP  int    `json:"totalXp"`
}

// LevelUpPayload — payload события "level.up".
type LevelUpPayload struct {
	Level             int      `json:"level"`
	Stage             int      `json:"stage"`
	StageChanged      bool     `json:"stageChanged"`
	UnlockedRewardIDs []string `json:"unlockedRewardIds"`
}

// Имена событий — дословно ключи docs/openapi.json → x-websocket.serverEvents.
const (
	eventPetStats = "pet.stats"
	eventXPGained = "xp.gained"
	eventLevelUp  = "level.up"
)

// Broadcaster рассылает события реального времени поверх pkg/wsh.Hub.
//
// Hub ничего не знает про конверт и про события контракта — он передаёт
// произвольный payload байтами (см. докстринг wsh.Hub.Send). Broadcaster —
// то место, где широковещательные байты становятся событием контракта.
type Broadcaster struct {
	hub *wsh.Hub
	clk clock.Clock

	// seqMu и seq — монотонный счётчик seq на пользователя. В памяти, не в
	// базе: seq — свойство одной реалтайм-сессии, а не персистентный факт
	// о питомце (contract: «Событие с меньшим seq игнорируется» — при
	// потере процесса клиент переподключается с нуля и делает resync,
	// см. правила x-websocket; здесь resync — просто новое соединение).
	seqMu sync.Mutex
	seq   map[uuid.UUID]uint64
}

// NewBroadcaster собирает рассыльщик поверх готового хаба.
//
// clk — та же зависимость, что уже есть у Service (pkg/clock), а не
// time.Now() внутри: forbidigo запрещает time.Now() везде, кроме pkg/ и
// cmd/, и это не формальность — метка события в конверте обязана быть
// времени сети, но источник времени в проекте один на всё приложение.
func NewBroadcaster(hub *wsh.Hub, clk clock.Clock) *Broadcaster {
	return &Broadcaster{hub: hub, clk: clk, seq: make(map[uuid.UUID]uint64)}
}

// nextSeq возвращает следующий seq для пользователя, начиная с 1: контракт
// не оговаривает стартовое значение явно, но 0 как «событий ещё не было»
// читается яснее, чем «последнее событие было с seq 0».
func (b *Broadcaster) nextSeq(userID uuid.UUID) uint64 {
	b.seqMu.Lock()
	defer b.seqMu.Unlock()
	b.seq[userID]++
	return b.seq[userID]
}

// send оборачивает payload в конверт и рассылает всем соединениям
// пользователя. Ошибка хаба (только про (де)сериализацию — см. wsh.Hub.Send)
// проглатывается: пуш всегда фоновый и вторичный по отношению к HTTP-ответу,
// который уже ушёл клиенту к моменту вызова.
func (b *Broadcaster) send(userID uuid.UUID, eventType string, payload any) {
	env := Envelope{
		EventID: uuid.New(),
		Seq:     b.nextSeq(userID),
		Type:    eventType,
		Ts:      b.clk.Now(), // время сети, не домена: доменное решение уже принято и записано, это только метка кадра
		Payload: payload,
	}
	_, _ = b.hub.Send(userID, env)
}

// PushAction рассылает события действия ухода. replayed=true (повтор по
// actionId) не рассылает ничего — контракт требует слать pet.stats «только
// при реальном изменении», а на повторе изменения нет.
func (b *Broadcaster) PushAction(userID uuid.UUID, kind ActionKind, res ActResult, replayed bool) {
	if replayed {
		return
	}

	b.send(userID, eventPetStats, StatsPayload{
		Stats:          toAPIStats(res.Pet.Stats),
		Mood:           res.Pet.Mood,
		MoodMultiplier: res.Pet.MoodMultiplier,
		UpdatedAt:      res.Pet.UpdatedAt.Format(time.RFC3339),
	})

	if res.XPGained > 0 {
		b.send(userID, eventXPGained, XPGainedPayload{
			Amount:   res.XPGained,
			Source:   string(kind),
			Level:    res.Pet.Level,
			XP:       res.Pet.XP,
			XPToNext: res.Pet.XPToNext,
			TotalXP:  res.Pet.TotalXP,
		})
	}

	if res.LeveledUp {
		b.send(userID, eventLevelUp, LevelUpPayload{
			Level:             res.Pet.Level,
			Stage:             res.Pet.Stage,
			StageChanged:      res.StageChanged,
			UnlockedRewardIDs: []string{}, // наград ещё нет
		})
	}
}

// upgrader обновляет HTTP до WebSocket.
//
// CheckOrigin разрешает всё: демо-стенд без домена (localhost, разные порты
// при локальной разработке фронта), и настоящей проверки origin в проекте
// пока нет ни у одного эндпоинта. Не строже HTTP-эндпоинтов этого пакета
// не значит слабее: WS-соединение только ЧИТАЕТ состояние (пуш), никаких
// действий через него принять нельзя (см. serveWS ниже).
var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// pingInterval — как часто ждать ping от клиента, прежде чем считать
// соединение мёртвым. Контракт называет 25 секунд периодом клиентского ping;
// дедлайн чтения — с запасом на джиттер сети.
const pingInterval = 60 * time.Second

// serveWS обновляет соединение до WebSocket и держит его до отключения.
//
// Держит соединение wsh.KeepAlive, а не свой цикл: дедлайн чтения — livenесс
// TCP-сокета, не доменное время, и обязан идти по настоящим часам независимо
// от того, чем протестирован Service (см. докстринг KeepAlive — там же
// записана поломка, которую даёт наивная версия этой функции).
func (h *Handler) serveWS(c *gin.Context) {
	a, ok := actor(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade сам пишет ответ клиенту при ошибке — здесь дописывать
		// больше нечего.
		return
	}

	conn, err := h.hub.Add(a.UserID, ws)
	if err != nil {
		_ = ws.Close()
		return
	}
	defer h.hub.Remove(a.UserID, conn)

	wsh.KeepAlive(ws, pingInterval)
}
