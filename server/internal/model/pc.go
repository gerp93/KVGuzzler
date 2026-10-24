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
	// Cost is the energy at the period's average price per kWh; null when no price is known. An estimate, like the energy.
	Cost *string `json:"cost"`
}

type PcGap struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Reason  string    `json:"reason"`
	PlugKwh *float64  `json:"plug_kwh"`
}

type PcLiveReading struct {
	Name  string  `json:"name"`
	Scope string  `json:"scope"`
	Kind  string  `json:"kind"`
	Watts float64 `json:"watts"`
}

type PcLiveProgram struct {
	Name     string  `json:"name"`
	CPUWatts float64 `json:"cpu_watts"`
	GPUWatts float64 `json:"gpu_watts"`
}

// PcLive is the PC's latest readings. ReceivedAt is when the server got it (by the server's clock) and is not sent on; AgeSeconds is
// what the dashboard sees instead.
type PcLive struct {
	At           time.Time       `json:"at"`
	Readings     []PcLiveReading `json:"readings"`
	Programs     []PcLiveProgram `json:"programs"`
	Notes        []string        `json:"notes"`
	IdleCPUWatts *float64        `json:"idle_cpu_watts"`
	IdleGPUWatts *float64        `json:"idle_gpu_watts"`
	AgeSeconds   float64         `json:"age_seconds"`
	ReceivedAt   time.Time       `json:"-"`
}

type PcRun struct {
	Name          string    `json:"name"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	CPUKwh        float64   `json:"cpu_kwh"`
	GPUKwh        float64   `json:"gpu_kwh"`
	ActiveSeconds float64   `json:"active_seconds"`
	StillRunning  bool      `json:"still_running"`
	// Cost is the run's energy at the price on the day it ended; null when no price is known.
	Cost *string `json:"cost"`
}

// PcSeriesLine is one reading of the PC over time. Lines are never added together: the wall line already contains the sensors, and a
// meter is a separate device.
type PcSeriesLine struct {
	Name   string  `json:"name"`
	Scope  string  `json:"scope"`
	Kind   string  `json:"kind"`
	Points []Point `json:"points"`
}

// PcSeries is what the PC's collector reported over time, one line per sensor, the wall plug and each meter, plus the stretches it
// was not recording so a chart can shade them instead of drawing a line across them.
type PcSeries struct {
	Bucket Bucket         `json:"bucket"`
	Lines  []PcSeriesLine `json:"lines"`
	Gaps   []PcGap        `json:"gaps"`
}

// PcHistoryPeriod is one row of a computer's energy history: a rolling period ending now. With its wall plug the headline is the whole PC
// (what the bill is made of); without one it is what the sensors measured. The sensors' own figure, and the stretches nothing was
// recorded (by reason), say how much of it was measured.
type PcHistoryPeriod struct {
	Key              string    `json:"key"`
	Label            string    `json:"label"`
	From             time.Time `json:"from"`
	To               time.Time `json:"to"`
	KWh              float64   `json:"kwh"`
	Basis            string    `json:"basis"`
	Cost             *string   `json:"cost"`
	CostEstimated    bool      `json:"cost_estimated"`
	SensorsKWh       float64   `json:"sensors_kwh"`
	AsleepSeconds    float64   `json:"asleep_seconds"`
	OffSeconds       float64   `json:"off_seconds"`
	LostPowerSeconds float64   `json:"lost_power_seconds"`
	PlugGapKWh       float64   `json:"plug_gap_kwh"`
}

type ComputerHistory struct {
	Periods []PcHistoryPeriod `json:"periods"`
}

// AverageUse is what a computer really used per day over a window, counting the time it was off or asleep, so it projects what a bill
// would really be. It averages over the time actually recorded when recording started partway through the window.
type AverageUse struct {
	KWh             float64   `json:"kwh"`
	Days            float64   `json:"days"`
	KWhPerDay       float64   `json:"kwh_per_day"`
	AverageWatts    float64   `json:"average_watts"`
	WholePC         bool      `json:"whole_pc"`
	IncludesPC      bool      `json:"includes_pc"`
	From            time.Time `json:"from"`
	PeripheralCount int       `json:"peripheral_count"`
}

// ComputerEstimate is what the cost estimator needs: today's price per kWh, and (when at least an hour is recorded) the real usage.
type ComputerEstimate struct {
	Rate          *string     `json:"rate"`
	RateEstimated bool        `json:"rate_estimated"`
	Currency      string      `json:"currency"`
	Usage         *AverageUse `json:"usage"`
}

type PcSource struct {
	SourceID       string           `json:"source_id"`
	Name           string           `json:"name"`
	LastSeen       *time.Time       `json:"last_seen"`
	TotalPeakWatts *float64         `json:"total_peak_watts"`
	Components     []PcComponent    `json:"components"`
	Programs       []PcProgramTotal `json:"programs"`
	Gaps           []PcGap          `json:"gaps"`
	Live           *PcLive          `json:"live"`
	// CarbonG and CarbonEstimatedKWh are the carbon of what the PC's sensors measured over the period (the same figures a meter carries).
	CarbonG            *float64 `json:"carbon_g"`
	CarbonEstimatedKWh *float64 `json:"carbon_estimated_kwh"`
	Runs               []PcRun  `json:"runs"`
}

// Computer is a computer as the user sees it: a name, the collector that reports its insides (if any), the plug it is on
// (its whole-PC figure at the wall) and the plugs next to it (monitor, speakers).
type Computer struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	SourceID             *string    `json:"source_id"`
	WallMeterID        *string    `json:"wall_meter_id"`
	PeripheralMeterIDs []string   `json:"peripheral_meter_ids"`
	LastSeen             *time.Time `json:"last_seen"`
}

// ComputerDetail is everything about one computer for a period. Setup is the wall figure plus the peripherals, a separate
// labelled figure for the whole desk; none of it is added to the house or circuit figures.
type ComputerDetail struct {
	Computer      Computer         `json:"computer"`
	From          time.Time        `json:"from"`
	To            time.Time        `json:"to"`
	Wall          *MeterSummary  `json:"wall"`
	Peripherals   []MeterSummary `json:"peripherals"`
	SetupKWh      *float64         `json:"setup_kwh"`
	SetupCost     *string          `json:"setup_cost"`
	CostEstimated bool             `json:"cost_estimated"`
	PC            *PcSource        `json:"pc"`
}

// OpenEiStatus says whether an OpenEI API key is saved. The key itself is never sent back.
type OpenEiStatus struct {
	HasKey bool `json:"has_key"`
}

// TariffInfo is what a utility's published tariff looks like to a client: who, what, what it costs. The rate grid itself stays on the server.
type TariffInfo struct {
	Label        string    `json:"label"`
	Utility      string    `json:"utility"`
	Name         string    `json:"name"`
	URI          *string   `json:"uri"`
	FetchedAt    time.Time `json:"fetched_at"`
	Summary      string    `json:"summary"`
	Cheapest     float64   `json:"cheapest"`
	Dearest      float64   `json:"dearest"`
	VariesByTime bool      `json:"varies_by_time"`
	Periods      int       `json:"periods"`
	Tiers        int       `json:"tiers"`
	FixedCharge  *string   `json:"fixed_charge"`
}

// TariffCandidate is one rate found for an address, ready to be chosen.
type TariffCandidate struct {
	TariffInfo
	IsDefault bool `json:"is_default"`
	Approved  bool `json:"approved"`
}

type TariffCandidates struct {
	Candidates []TariffCandidate `json:"candidates"`
}

// CarbonConfig is the carbon-footprint setup as a client sees it. The key is never part of it.
type CarbonConfig struct {
	Configured  bool       `json:"configured"`
	Place       string     `json:"place,omitempty"`
	LastOutcome *string    `json:"last_outcome"`
	LastFetch   *time.Time `json:"last_fetch"`
	LastGrams   *float64   `json:"last_grams"`
	LastHour    *time.Time `json:"last_hour"`
}
