module tamagochi

// 1.26.2 was breaking golangci-lint (no available build lints a module that
// targets a newer Go than it was built with) — pinned back to the newest
// version current tooling actually supports. Nothing here uses 1.26-only
// features, so this is lossless.
go 1.25.0

require github.com/gofrs/uuid v4.4.0+incompatible

require github.com/google/uuid v1.6.0 // indirect
