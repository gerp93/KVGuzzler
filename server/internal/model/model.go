// Package model holds the types shared by the server's parts. Their JSON shapes follow contract/openapi.yaml.
package model

import "time"

// Scope is what a channel measures. Scopes are never summed across each other: the house is the mains channels
// alone, a circuit or plug is a part of it, and the PC's wall plug is a slice of some circuit, added to nothing.
type Scope string

const (
	ScopeMains   Scope = "mains"
	ScopeCircuit Scope = "circuit"
	ScopePlug    Scope = "plug"
	ScopePC      Scope = "pc"
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

type Channel struct {
	ID       string   `json:"id"`
	DeviceID string   `json:"device_id"`
	Index    *int     `json:"index"`
	Scope    Scope    `json:"scope"`
	Label    string   `json:"label"`
	Enabled  bool     `json:"enabled"`
	MaxWatts *float64 `json:"max_watts"`
	// ComputerID is the computer this plug belongs to (its wall plug if the scope is pc, a peripheral if plug), or nil.
	ComputerID *string `json:"computer_id"`
	// GroupID is the appliance group this plug is part of (shown as one appliance with the group's other plugs), or nil.
	GroupID *string `json:"group_id"`
}

type Reading struct {
	ChannelID string   `json:"channel_id"`
	Watts     float64  `json:"watts"`
	Volts     *float64 `json:"volts"`
	Amps      *float64 `json:"amps"`
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
	ChannelID string  `json:"channel_id"`
	Bucket    Bucket  `json:"bucket"`
	Points    []Point `json:"points"`
}

type ChannelSummary struct {
	ChannelID      string   `json:"channel_id"`
	ComputerID     *string  `json:"computer_id"`
	GroupID        *string  `json:"group_id"`
	Scope          Scope    `json:"scope"`
	KWh            float64  `json:"kwh"`
	Cost           *string  `json:"cost"`
	CarbonG        *float64 `json:"carbon_g"`
	PeakWatts      *float64 `json:"peak_watts"`
	MonitoredHours float64  `json:"monitored_hours"`
}

type Summary struct {
	From          time.Time        `json:"from"`
	To            time.Time        `json:"to"`
	Channels      []ChannelSummary `json:"channels"`
	HouseKWh      *float64         `json:"house_kwh"`
	HouseCost     *string          `json:"house_cost"`
	CostEstimated bool             `json:"cost_estimated"`
	UnmeteredKWh  *float64         `json:"unmetered_kwh"`
}
