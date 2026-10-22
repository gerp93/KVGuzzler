// Package source is what the collector polls: one device and the channels it measures.
package source

import (
	"context"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

type Source interface {
	// Device describes the device. Online and LastSeen are filled in by the collector, not here.
	Device() model.Device
	// Channels lists what the device measures, with the sanity cap of each (a reading above it is dropped).
	Channels() []model.Channel
	// Poll reads every channel once. It must never change anything on the device.
	Poll(ctx context.Context) ([]model.Reading, error)
}
