package model

import "time"

// ApplianceGroup is several plugs shown as one appliance (an entertainment center on two power strips).
type ApplianceGroup struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ChannelIDs []string `json:"channel_ids"`
}

// GroupDetail is a group's figures for a period: the energy of each plug and their total.
type GroupDetail struct {
	Group         ApplianceGroup   `json:"group"`
	From          time.Time        `json:"from"`
	To            time.Time        `json:"to"`
	Members       []ChannelSummary `json:"members"`
	TotalKWh      float64          `json:"total_kwh"`
	TotalCost     *string          `json:"total_cost"`
	CostEstimated bool             `json:"cost_estimated"`
}
