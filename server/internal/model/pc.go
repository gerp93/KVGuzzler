package model

import "time"

// The PC's own view, as the PC collector reported it. Never added to the house or circuit figures.

type PcComponent struct {
	Name           string   `json:"name"`
	Scope          string   `json:"scope"`
	Kind           string   `json:"kind"`
	KWh            float64  `json:"kwh"`
	AvgWatts       *float64 `json:"avg_watts"`
	PeakWatts      *float64 `json:"peak_watts"`
	MonitoredHours float64  `json:"monitored_hours"`
}

type PcProgramTotal struct {
	Name          string  `json:"name"`
	CPUKwh        float64 `json:"cpu_kwh"`
	GPUKwh        float64 `json:"gpu_kwh"`
	KWh           float64 `json:"kwh"`
	ActiveSeconds float64 `json:"active_seconds"`
}

type PcGap struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Reason  string    `json:"reason"`
	PlugKwh *float64  `json:"plug_kwh"`
}

type PcSource struct {
	SourceID       string           `json:"source_id"`
	Name           string           `json:"name"`
	LastSeen       *time.Time       `json:"last_seen"`
	TotalPeakWatts *float64         `json:"total_peak_watts"`
	Components     []PcComponent    `json:"components"`
	Programs       []PcProgramTotal `json:"programs"`
	Gaps           []PcGap          `json:"gaps"`
}

// Computer is a computer as the user sees it: a name, the collector that reports its insides (if any), the plug it is on
// (its whole-PC figure at the wall) and the plugs next to it (monitor, speakers).
type Computer struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	SourceID             *string    `json:"source_id"`
	WallChannelID        *string    `json:"wall_channel_id"`
	PeripheralChannelIDs []string   `json:"peripheral_channel_ids"`
	LastSeen             *time.Time `json:"last_seen"`
}

// ComputerDetail is everything about one computer for a period. Setup is the wall figure plus the peripherals, a separate
// labelled figure for the whole desk; none of it is added to the house or circuit figures.
type ComputerDetail struct {
	Computer      Computer         `json:"computer"`
	From          time.Time        `json:"from"`
	To            time.Time        `json:"to"`
	Wall          *ChannelSummary  `json:"wall"`
	Peripherals   []ChannelSummary `json:"peripherals"`
	SetupKWh      *float64         `json:"setup_kwh"`
	SetupCost     *string          `json:"setup_cost"`
	CostEstimated bool             `json:"cost_estimated"`
	PC            *PcSource        `json:"pc"`
}
