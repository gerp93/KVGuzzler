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

	// Tariff and Profile are the looked-up tariff and when the power is used. They are kept apart from the saved configuration (the
	// grid is big and the profile is learned), so they are filled in by whoever loads the configuration for pricing.
	Tariff  *Tariff       `json:"-"`
	Profile *UsageProfile `json:"-"`
	// Frozen is the price each finished day was saved with (see Schedule.WithFrozen). Like the tariff it is not part of the saved settings.
	Frozen map[Date]Money `json:"-"`
}

// DefaultConfig is one rate, none set yet: energy is counted but nothing is priced.
func DefaultConfig() Config { return Config{Mode: ModeOne, Currency: "USD", Bills: []Bill{}} }

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Validate says why a configuration cannot be saved, in words fit to show, or returns nil.
func (c Config) Validate() error {
	switch c.Mode {
	case ModeOne, ModeBills, ModeLookup:
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

// Schedule is the schedule this configuration describes, with days cut at local midnight in loc. A day that is over keeps the price it
// was saved with (Frozen), so only today and the days to come follow the settings.
func (c Config) Schedule(loc *time.Location) *Schedule {
	return c.Live(loc).WithFrozen(c.Frozen)
}

// Live is the schedule as the settings alone price it, ignoring what finished days were saved with. It is what a day is saved at, and what
// "re-price" compares the saved days against.
func (c Config) Live(loc *time.Location) *Schedule {
	return NewSchedule(c.Mode, c.FixedRate, c.Bills, loc).WithTariff(c.Tariff, c.Profile)
}
