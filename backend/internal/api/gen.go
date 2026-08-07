// Package api содержит типы, сгенерированные из docs/openapi.json.
//
// Руками здесь ничего не пишут: файл types.gen.go перезаписывается `make gen`,
// а CI-джоб contract-drift падает, если сгенерированное разошлось со спекой.
// Меняешь форму запроса или ответа — правь спеку и перегенерируй.
//
// Генерируются ТОЛЬКО типы, без серверных интерфейсов: gin-server выдал бы
// ServerInterface на все операции контракта разом, и сборка падала бы у любого,
// кто реализовал не все, — то есть у обеих веток фич в момент мержа.
//
// Почему версия запинена в директиве, а не ставится глобально —
// docs/DECISIONS.md, «Померено, но не взято».
package api

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0 -generate types -package api -o types.gen.go ../../../docs/openapi.json
