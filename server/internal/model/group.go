package model

import "time"

// ApplianceGroup is a logical grouping of meters shown as one (an entertainment center on two power strips, a desk with its computer): it
// does not change where any of them is in the wiring.
type ApplianceGroup struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	MeterIDs []string `json:"meter_ids"`
}

// GroupDetail is a group's figures for a period: the energy of each plug and their total.
type GroupDetail struct {
	Group         ApplianceGroup `json:"group"`
	From          time.Time      `json:"from"`
	To            time.Time      `json:"to"`
	Members       []MeterSummary `json:"members"`
	TotalKWh      float64        `json:"total_kwh"`
	TotalCost     *string        `json:"total_cost"`
	CostEstimated bool           `json:"cost_estimated"`
	// NotAdded lists the members left out of the total because another member already contains them (a plug on a circuit that is in the same
	// group): they are still listed, but their energy is already in the one that holds them, so nothing is counted twice.
	NotAdded []string `json:"not_added"`
}
