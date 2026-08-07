// Package authctx хранит идентификатор пользователя в context.Context.
//
// Ключ — неэкспортируемый тип struct{}, а не строковая константа. Это не
// стилистика. В feat/pet-service объявлены две константы одного типа ctxKey —
// requestIDKey и RequestIDKey — с одним и тем же значением "request_id", то
// есть это один ключ; WS-обработчик читает по нему «идентификатор
// пользователя» и получает идентификатор запроса. С типом struct{} два разных
// ключа невозможно случайно сделать одинаковыми: тип уникален по объявлению,
// а не по значению.
package authctx

import (
	"context"

	"github.com/google/uuid"
)

// userIDKey — тип-ключ. Неэкспортируемый: положить значение в контекст можно
// только через WithUserID, то есть чужой пакет не подменит его своим.
type userIDKey struct{}

// WithUserID возвращает контекст с идентификатором пользователя.
func WithUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

// UserID достаёт идентификатор пользователя из контекста.
//
// Второе значение — false, если идентификатора нет ИЛИ он нулевой. uuid.Nil
// здесь не «какой-то пользователь», а его отсутствие: именно возможность
// молча получить пустой ключ и раздавать по нему соединения делает адресный
// пуш невозможным, не сломав при этом ни сборку, ни тесты.
func UserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	if !ok || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
}
