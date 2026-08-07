// Package httpx пишет HTTP-ответы в конверте контракта.
//
// Зачем отдельный пакет, а не gin.H на месте: конверт описан в
// docs/openapi.json (Envelope / ErrorEnvelope / Meta) и разбирается фронтом
// единообразно — там же лежит машинный код ошибки, по которому клиент ветвится.
// Обработчик, отвечающий gin.H{"error": ..., "message": ...}, синтаксически
// корректен, проходит линтер и при этом не разбирается ни одним клиентом
// контракта; ровно так отвечает WS-обработчик в feat/pet-service.
//
// Инвариант, который здесь держится конструкцией, а не соглашением:
// Meta.Code всегда равен HTTP-статусу ответа. Контракт требует этого дословно
// («ВСЕГДА число, дублирует HTTP-статус»), а разойтись они могут только если
// статус и тело собирают в двух разных местах — поэтому статус тут ровно один
// аргумент, из которого берётся и то, и другое.
//
// Пакет лежит в internal/, а не в pkg/: он импортирует internal/api
// (сгенерированные из контракта типы), а pkg/ по правилу depguard
// platform-layer ниже фич и не импортирует internal/**. Имя файла не
// handler.go и не service.go, поэтому слоевые правила его не касаются — он
// не слой фичи, а общий для фич способ ответить.
package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	oatypes "github.com/oapi-codegen/runtime/types"

	"tamagochi/internal/api"
)

// requestIDHeader — заголовок, который эхом уходит в Meta.RequestId.
// По нему бэкенд находит лог по скриншоту бага (описание поля в контракте).
const requestIDHeader = "X-Request-Id"

// contentType выставляется до WriteHeader: после отправки статуса заголовки
// уже ушли, и Set молча ничего не делает.
const contentType = "application/json; charset=utf-8"

// fallbackBody — ответ на случай, когда payload не сериализуется.
//
// Это литерал, а не собранная структура, намеренно: сюда попадают уже после
// того, как json.Marshal один раз отказал, и вторая попытка что-то собрать
// может отказать так же. Литерал валиден по контракту по построению.
const fallbackBody = `{"data":null,"meta":{"code":500,"message":"Внутренняя ошибка сервера","error":"INTERNAL_ERROR"}}`

// OK пишет успешный ответ: data кладётся в конверт как есть.
//
// message — текст для пользователя на русском (так требует контракт), а не
// для разработчика: он может оказаться в интерфейсе.
func OK(w http.ResponseWriter, r *http.Request, status int, data any, message string) {
	write(w, status, api.Envelope{
		Data: data,
		Meta: meta(r, status, message, nil),
	})
}

// Fail пишет ответ об ошибке: data остаётся пустой, машинный код уходит в
// Meta.Error.
func Fail(w http.ResponseWriter, r *http.Request, status int, code api.ErrorCode, message string) {
	write(w, status, api.ErrorEnvelope{
		Meta: meta(r, status, message, &code),
	})
}

// meta собирает Meta. Code берётся из того же status, что уйдёт в WriteHeader.
func meta(r *http.Request, status int, message string, code *api.ErrorCode) api.Meta {
	m := api.Meta{
		Code:    status,
		Message: message,
		Error:   code,
	}
	if id, ok := requestID(r); ok {
		m.RequestId = &id
	}
	return m
}

// requestID достаёт X-Request-Id, если он есть и разбирается как UUID.
//
// Кривой заголовок не ошибка запроса: контракт объявляет поле необязательным,
// а формат — uuid. Класть туда произвольную строку от клиента нельзя, иначе
// ответ перестанет проходить валидацию по собственной спеке.
func requestID(r *http.Request) (oatypes.UUID, bool) {
	if r == nil {
		return oatypes.UUID{}, false
	}
	raw := r.Header.Get(requestIDHeader)
	if raw == "" {
		return oatypes.UUID{}, false
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return oatypes.UUID{}, false
	}
	return parsed, true
}

// write сериализует тело ДО отправки статуса.
//
// Порядок важен: если сначала выставить статус, а потом обнаружить, что тело
// не сериализуется, клиент получит 200 с обрывком — то есть успех, которого
// не было. Здесь неудачная сериализация превращается в честный 500.
func write(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		encoded = []byte(fallbackBody)
		status = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	// Ошибку записи обрабатывать некуда: статус и заголовки уже отправлены,
	// сказать клиенту что-то ещё нельзя. Соединение закроет http.Server.
	_, _ = w.Write(encoded)
}
