package authctx_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"tamagochi/pkg/authctx"
)

func TestRoundTrip(t *testing.T) {
	want := uuid.New()

	got, ok := authctx.UserID(authctx.WithUserID(context.Background(), want))
	if !ok {
		t.Fatal("идентификатор не найден сразу после того, как был положен")
	}
	if got != want {
		t.Errorf("получили %s, ожидали %s", got, want)
	}
}

func TestMissing(t *testing.T) {
	if _, ok := authctx.UserID(context.Background()); ok {
		t.Error("пустой контекст отдал идентификатор")
	}
}

// Нулевой UUID — это отсутствие пользователя, а не пользователь.
//
// Проверка не косметическая: в feat/pet-service WS-обработчик кладёт
// соединения по ключу, который при отсутствии значения равен пустой строке,
// и все пользователи оказываются одним. Здесь такой ключ не проходит.
func TestNilUUIDIsNotAUser(t *testing.T) {
	ctx := authctx.WithUserID(context.Background(), uuid.Nil)

	if id, ok := authctx.UserID(ctx); ok {
		t.Errorf("uuid.Nil принят за пользователя: %s", id)
	}
}

// Чужое значение под чужим ключом не должно подменять идентификатор.
//
// Ключ authctx неэкспортируемого типа, поэтому собрать коллизию снаружи
// нельзя в принципе — тест фиксирует, что это свойство сохраняется: строковый
// ключ с любым содержимым проходит мимо.
func TestForeignKeyDoesNotCollide(t *testing.T) {
	type otherStructKey struct{}
	// Именно так объявлен ключ в feat/pet-service: строковый тип со значением.
	// Два таких ключа с одинаковым значением совпадают — там это и произошло.
	type foreignStringKey string

	want := uuid.New()
	ctx := authctx.WithUserID(context.Background(), want)
	ctx = context.WithValue(ctx, otherStructKey{}, uuid.New())
	ctx = context.WithValue(ctx, foreignStringKey("user_id"), uuid.New())

	got, ok := authctx.UserID(ctx)
	if !ok {
		t.Fatal("идентификатор потерялся после добавления чужих ключей")
	}
	if got != want {
		t.Errorf("чужой ключ подменил идентификатор: получили %s, ожидали %s", got, want)
	}
}

// Значение неверного типа под ключом пакета появиться не может, но UserID
// обязан пережить и это, не паникуя: type assertion — с comma-ok.
func TestOverwriteWithSameKeyKeepsLastValue(t *testing.T) {
	first, second := uuid.New(), uuid.New()

	ctx := authctx.WithUserID(context.Background(), first)
	ctx = authctx.WithUserID(ctx, second)

	got, ok := authctx.UserID(ctx)
	if !ok || got != second {
		t.Errorf("получили (%s, %v), ожидали (%s, true)", got, ok, second)
	}
}
