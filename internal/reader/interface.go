package reader

import (
	"RedisShake/internal/entry"
	"RedisShake/internal/status"
	"context"
)

type Reader interface {
	status.Statusable
	StartRead(ctx context.Context) []chan *entry.Entry
}

// ScanCountSetter is implemented by readers whose SCAN COUNT can be changed at runtime.
type ScanCountSetter interface {
	SetScanCount(count int)
}
