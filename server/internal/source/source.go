// Package source is what the collector polls: one device and the meters it measures.
package source

import (
	"context"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// Noter is a source that can also say, in words, what needs attention (a clamp put on backwards, say). The collector shows these beside
// its own notes.
type Noter interface {
	Notes() []string
}

type Source interface {
	// Device describes the device. Online and LastSeen are filled in by the collector, not here.
	Device() model.Device
	// Meters lists what the device measures, with the sanity cap of each (a reading above it is dropped).
	Meters() []model.Meter
	// Poll reads every meter once. It must never change anything on the device.
	Poll(ctx context.Context) ([]model.Reading, error)
}
