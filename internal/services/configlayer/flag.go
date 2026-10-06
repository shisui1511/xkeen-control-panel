package configlayer

import "context"

// Enable запускает сборку файлов после включения слоя.
func (l *Layer) Enable() {}

// Disable выключает слой: файлы панели уходят в набор копий.
func (l *Layer) Disable(ctx context.Context) error { return errNotImplemented }
