package model

import "time"

// FordLink is the setup of reading an electric vehicle's odometer from Ford and what the last read said. It never holds the client secret or a token.
type FordLink struct {
	// Configured is whether a client ID, secret and the addresses are saved; Linked whether the owner has agreed to share the vehicle (a refresh
	// token is held). NeedsLink says the permission was lost (expired or revoked) and has to be given again.
	Configured   bool       `json:"configured"`
	Linked       bool       `json:"linked"`
	NeedsLink    bool       `json:"needs_link"`
	ClientID     string     `json:"client_id,omitempty"`
	AuthorizeURL string     `json:"authorize_url,omitempty"`
	TokenURL     string     `json:"token_url,omitempty"`
	OdometerUnit string     `json:"odometer_unit,omitempty"`
	MeterID      string     `json:"meter_id,omitempty"`
	LastCheck    *time.Time `json:"last_check"`
	LastOutcome  *string    `json:"last_outcome"`
	// LastRaw is the figure as Ford gave it, LastFound where in its answer it was, LastMiles what it came to and LastDay the day it was saved for.
	LastRaw   *float64 `json:"last_raw"`
	LastFound *string  `json:"last_found"`
	LastMiles *float64 `json:"last_miles"`
	LastDay   *string  `json:"last_day"`
}
