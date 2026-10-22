package rates

import (
	"errors"
	"regexp"
	"time"
)

// Config is how the price of a kWh is worked out, as the user set it (the contract's RateConfig).
type Config struct {
	Mode        Mode    `json:"mode"`
	Currency    string  `json:"currency,omitempty"`
	FixedRate   *Money  `json:"fixed_rate,omitempty"`
	Bills       []Bill  `json:"bills"`
	TariffLabel *string `json:"tariff_label,omitempty"`
}

// DefaultConfig is one rate, none set yet: energy is counted but nothing is priced.
func DefaultConfig() Config { return Config{Mode: ModeOne, Currency: "USD", Bills: []Bill{}} }

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Validate says why a configuration cannot be saved, in words fit to show, or returns nil.
func (c Config) Validate() error {
	switch c.Mode {
	case ModeOne, ModeBills:
	case ModeLookup:
		return errors.New("Looking a rate up from a utility's published tariff is not built in the house server yet.")
	default:
		return errors.New("The price source is one rate or my bills.")
	}
	if c.Currency != "" && !currencyPattern.MatchString(c.Currency) {
		return errors.New("The currency is a three-letter code like USD.")
	}
	if c.FixedRate != nil && c.FixedRate.Sign() < 0 {
		return errors.New("The rate cannot be negative.")
	}
	return ValidateBills(c.Bills)
}

// Normalized fills in what was left out (the currency, an empty bills list) so what is stored and returned is complete.
func (c Config) Normalized() Config {
	if c.Currency == "" {
		c.Currency = "USD"
	}
	if c.Bills == nil {
		c.Bills = []Bill{}
	}
	return c
}

// Schedule is the schedule this configuration describes, with days cut at local midnight in loc.
func (c Config) Schedule(loc *time.Location) *Schedule {
	return NewSchedule(c.Mode, c.FixedRate, c.Bills, loc)
}
