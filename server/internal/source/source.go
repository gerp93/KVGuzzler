// Package source is what the collector polls: one device and the meters it measures.
package source

import (
	"context"
	"time"

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

// BootCause is why a device last started, as far as the device says.
type BootCause int

const (
	// BootUnknown: the device did not say.
	BootUnknown BootCause = iota
	// BootPower: power was applied (it was off, or browned out).
	BootPower
	// BootOther: a restart for some other reason (a firmware update, a software restart).
	BootOther
)

// Boot is when a device last started. A device that loses power starts again when it returns, so for a plug the time is when power came back.
type Boot struct {
	DeviceID string
	Name     string
	At       time.Time
	Cause    BootCause
}

// Booter is a source whose device can say when it last started. Asking is a read and changes nothing on the device.
type Booter interface {
	Boot(ctx context.Context) (Boot, error)
}
