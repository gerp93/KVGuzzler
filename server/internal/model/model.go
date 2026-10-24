// Package model holds the types shared by the server's parts. Their JSON shapes follow contract/openapi.yaml.
package model

import "time"

// Scope is what a meter measures. Scopes are never summed across each other: the house is the mains meters
// alone, a circuit or plug is a part of it, and the PC's wall plug is a slice of some circuit, added to nothing.
type Scope string

const (
	ScopeMains   Scope = "mains"
	ScopeCircuit Scope = "circuit"
	ScopePlug    Scope = "plug"
	ScopePC      Scope = "pc"
)

// MeterType says what a meter is in the wiring, as the user would name it: the whole house, a sub-panel's feed (it holds other circuits),
// one breaker, or a device (a plug, the PC's wall plug). It is a description; what the math needs (which meter is the house, what is
// subtracted from what) still comes from the scope and the tree.
type MeterType string

const (
	TypeHouse    MeterType = "house"
	TypeSubPanel MeterType = "sub_panel"
	TypeCircuit  MeterType = "circuit"
	TypeDevice   MeterType = "device"
)

// Bucket is the width of one point in a series.
type Bucket string

const (
	BucketMinute Bucket = "minute"
	BucketHour   Bucket = "hour"
	BucketDay    Bucket = "day"
)

type Device struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Host     string     `json:"host"`
	Name     string     `json:"name,omitempty"`
	Model    string     `json:"model,omitempty"`
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"last_seen"`
}

type Meter struct {
	ID       string   `json:"id"`
	DeviceID string   `json:"device_id"`
	Index    *int     `json:"index"`
	Scope    Scope    `json:"scope"`
	Label    string   `json:"label"`
	Enabled  bool     `json:"enabled"`
	MaxWatts *float64 `json:"max_watts"`
	// Type is what the meter is (see MeterType). The server fills it in: a Refoss meter's is the user's choice, the rest follow from the scope,
	// and a circuit with circuits inside it is a sub-panel until the user says otherwise.
	Type MeterType `json:"type"`
	// ComputerID is the computer this plug belongs to (its wall plug if the scope is pc, a peripheral if plug), or nil.
	ComputerID *string `json:"computer_id"`
	// GroupID is the appliance group this plug is part of (shown as one appliance with the group's other plugs), or nil.
	GroupID *string `json:"group_id"`
	// Invert flips the sign of a clamp's reading (a Refoss clamp put on backwards). Only Refoss meters have it.
	Invert bool `json:"invert"`
	// PairWith is the meter of the other leg of the same 240 V breaker: this meter then reports both legs added, and that one is
	// not reported on its own. Only Refoss meters have it.
	PairWith *string `json:"pair_with"`
	// ParentID is the circuit this one is part of: a circuit that feeds a sub-panel holds the circuits of that panel. Its own figure already
	// contains theirs, so a parent and its children are never added together. Only a circuit has one.
	ParentID *string `json:"parent_id"`
	// Favorite marks a meter the user wants one click away: it appears as a choice on the live page. Kept here so every device sees the same ones.
	Favorite bool `json:"favorite"`
}

type Reading struct {
	MeterID string   `json:"meter_id"`
	Watts   float64  `json:"watts"`
	Volts   *float64 `json:"volts"`
	Amps    *float64 `json:"amps"`
}

type Live struct {
	Time       time.Time `json:"time"`
	Readings   []Reading `json:"readings"`
	HouseWatts *float64  `json:"house_watts"`
	Notes      []string  `json:"notes"`
}

type Point struct {
	T        time.Time `json:"t"`
	AvgWatts float64   `json:"avg_watts"`
	MaxWatts float64   `json:"max_watts"`
	KWh      float64   `json:"kwh"`
}

type Series struct {
	MeterID string  `json:"meter_id"`
	Bucket  Bucket  `json:"bucket"`
	Points  []Point `json:"points"`
}

type MeterSummary struct {
	MeterID    string   `json:"meter_id"`
	ComputerID *string  `json:"computer_id"`
	GroupID    *string  `json:"group_id"`
	Scope      Scope    `json:"scope"`
	KWh        float64  `json:"kwh"`
	Cost       *string  `json:"cost"`
	CarbonG    *float64 `json:"carbon_g"`
	// CarbonEstimatedKWh is the part of KWh whose carbon was worked out at an average because its own hour had no saved grid intensity.
	CarbonEstimatedKWh *float64 `json:"carbon_estimated_kwh"`
	PeakWatts          *float64 `json:"peak_watts"`
	MonitoredHours     float64  `json:"monitored_hours"`
}

type Summary struct {
	From          time.Time      `json:"from"`
	To            time.Time      `json:"to"`
	Meters        []MeterSummary `json:"meters"`
	HouseKWh      *float64       `json:"house_kwh"`
	HouseCost     *string        `json:"house_cost"`
	CostEstimated bool           `json:"cost_estimated"`
	UnmeteredKWh  *float64       `json:"unmetered_kwh"`
	// HousePeakWatts is the mains' highest draws added: an upper bound, as the two legs need not peak in the same second.
	HousePeakWatts *float64 `json:"house_peak_watts"`
	// HouseCarbonG and HouseCarbonEstimatedKWh are the mains' carbon figures added (see MeterSummary).
	HouseCarbonG            *float64 `json:"house_carbon_g"`
	HouseCarbonEstimatedKWh *float64 `json:"house_carbon_estimated_kwh"`
}

// HouseHistoryPeriod is one row of the house's energy history: a rolling period ending now.
type HouseHistoryPeriod struct {
	Key                string    `json:"key"`
	Label              string    `json:"label"`
	From               time.Time `json:"from"`
	To                 time.Time `json:"to"`
	KWh                float64   `json:"kwh"`
	Cost               *string   `json:"cost"`
	CostEstimated      bool      `json:"cost_estimated"`
	PeakWatts          *float64  `json:"peak_watts"`
	CarbonG            *float64  `json:"carbon_g"`
	CarbonEstimatedKWh *float64  `json:"carbon_estimated_kwh"`
}

type HouseHistory struct {
	Periods []HouseHistoryPeriod `json:"periods"`
}
