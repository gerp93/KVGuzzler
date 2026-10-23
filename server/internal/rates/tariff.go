package rates

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// GridSize is 12 months x weekday/weekend x 24 hours.
const GridSize = 12 * 2 * 24

// Tariff is a utility's published electricity price, reduced to what can be used: the price of a kWh for each month, for weekdays and
// weekends, for each hour of the day. The energy charge only (the first tier's rate plus its adjustment and the month's fuel adjustment);
// fixed monthly charges and demand charges are not part of a price per kWh, as with a bill's flat fees. A port of KVGuzzler.Core.Billing.Tariff.
type Tariff struct {
	// Label is the rate's page label in the OpenEI Utility Rate Database, which finds the same rate again later.
	Label   string  `json:"label"`
	Utility string  `json:"utility"`
	Name    string  `json:"name"`
	URI     *string `json:"uri"`
	// FetchedAt is when it was read from OpenEI.
	FetchedAt time.Time `json:"fetched_at"`
	// Grid is 12 months x weekday/weekend x 24 hours of price per kWh, month-major: index (month*2 + weekend)*24 + hour.
	Grid []float64 `json:"grid"`
	// Periods is how many time-of-use periods the tariff has (1 is a flat rate by time of day).
	Periods int `json:"periods"`
	// Tiers is the most usage tiers any period has. Tiers depend on a whole house's monthly usage, which a PC's share cannot tell, so the
	// first tier is used and the Settings page says so when there is more than one.
	Tiers       int     `json:"tiers"`
	FixedCharge *string `json:"fixed_charge"`
}

// GridIndex is where a month (0 to 11), day kind and hour (0 to 23) sit in a Tariff's grid.
func GridIndex(month0 int, weekend bool, hour int) int {
	w := 0
	if weekend {
		w = 1
	}
	return (month0*2+w)*24 + hour
}

// Price is the price of a kWh in that month, kind of day and hour.
func (t *Tariff) Price(month0 int, weekend bool, hour int) float64 {
	return t.Grid[GridIndex(month0, weekend, hour)]
}

func (t *Tariff) Cheapest() float64 {
	lo := math.Inf(1)
	for _, p := range t.Grid {
		lo = math.Min(lo, p)
	}
	return lo
}

func (t *Tariff) Dearest() float64 {
	hi := math.Inf(-1)
	for _, p := range t.Grid {
		hi = math.Max(hi, p)
	}
	return hi
}

// VariesByTime is true when the price changes with the hour or the season, so the hours used matter.
func (t *Tariff) VariesByTime() bool { return t.Dearest()-t.Cheapest() > 1e-9 }

// Summary is a line for a list: who, what, and what it costs.
func (t *Tariff) Summary(currency string) string {
	if t.VariesByTime() {
		s := "s"
		if t.Periods == 1 {
			s = ""
		}
		return fmt.Sprintf("%s: %s · %s%.4f to %s%.4f per kWh, %d time-of-use period%s", t.Utility, t.Name, currency, t.Cheapest(), currency, t.Dearest(), t.Periods, s)
	}
	return fmt.Sprintf("%s: %s · %s%.4f per kWh", t.Utility, t.Name, currency, t.Cheapest())
}

// ParseTariff reads a saved tariff, or returns nil when the text is empty, damaged or not a usable grid.
func ParseTariff(text string) *Tariff {
	if text == "" {
		return nil
	}
	var t Tariff
	if err := json.Unmarshal([]byte(text), &t); err != nil || len(t.Grid) != GridSize {
		return nil
	}
	for _, p := range t.Grid {
		if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 {
			return nil
		}
	}
	return &t
}

// UsageProfile is when the electricity is used: the share of usage in each hour of a weekday and of a weekend day, learned from the saved
// history. It turns a time-of-use tariff into one price for a day (weighted by when power is really used), which is what the rest of the
// server prices with.
type UsageProfile struct {
	Weekday []float64 `json:"weekday"`
	Weekend []float64 `json:"weekend"`
}

// UniformProfile spreads the day evenly.
func UniformProfile() UsageProfile {
	even := func() []float64 {
		out := make([]float64, 24)
		for i := range out {
			out[i] = 1.0 / 24
		}
		return out
	}
	return UsageProfile{Weekday: even(), Weekend: even()}
}

// HourEnergy is the energy used in one hour (the unix second the hour starts).
type HourEnergy struct {
	Hour int64
	KWh  float64
}

// ProfileFromHourly is the profile of some hourly energy. A kind of day with no usage is spread evenly.
func ProfileFromHourly(hours []HourEnergy, loc *time.Location) UsageProfile {
	weekday, weekend := make([]float64, 24), make([]float64, 24)
	for _, h := range hours {
		if !(h.KWh > 0) || math.IsInf(h.KWh, 0) {
			continue
		}
		local := time.Unix(h.Hour, 0).In(loc)
		if wd := local.Weekday(); wd == time.Saturday || wd == time.Sunday {
			weekend[local.Hour()] += h.KWh
		} else {
			weekday[local.Hour()] += h.KWh
		}
	}
	return UsageProfile{Weekday: normalised(weekday), Weekend: normalised(weekend)}
}

func normalised(energy []float64) []float64 {
	var total float64
	for _, e := range energy {
		total += e
	}
	out := make([]float64, 24)
	for i, e := range energy {
		if total > 0 {
			out[i] = e / total
		} else {
			out[i] = 1.0 / 24
		}
	}
	return out
}

// ParseProfile reads a saved profile, or returns nil when the text is empty, damaged or not 24 hours for each kind of day.
func ParseProfile(text string) *UsageProfile {
	if text == "" {
		return nil
	}
	var p UsageProfile
	if err := json.Unmarshal([]byte(text), &p); err != nil || len(p.Weekday) != 24 || len(p.Weekend) != 24 {
		return nil
	}
	for _, w := range append(append([]float64(nil), p.Weekday...), p.Weekend...) {
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 {
			return nil
		}
	}
	return &p
}

// tariffRate is the price of a day under the looked-up tariff: each hour's price times the share of the day's usage in that hour.
func (s *Schedule) tariffRate(day Date) Money {
	weekend := day.Weekday() == time.Saturday || day.Weekday() == time.Sunday
	share := s.profile.Weekday
	if weekend {
		share = s.profile.Weekend
	}
	var price float64
	for hour := 0; hour < 24; hour++ {
		price += share[hour] * s.tariff.Price(int(day.Month)-1, weekend, hour)
	}
	return MoneyFromFloat(price)
}

// WithTariff gives the schedule a looked-up tariff (used in ModeLookup) and when the power is used to weight it into one price a day;
// a nil profile spreads the day evenly. Without a tariff, lookup mode falls back on the single rate.
func (s *Schedule) WithTariff(t *Tariff, p *UsageProfile) *Schedule {
	s.tariff = t
	profile := UniformProfile()
	if p != nil {
		profile = *p
	}
	s.profile = profile
	return s
}

func (s *Schedule) usesTariff() bool { return s.mode == ModeLookup && s.tariff != nil }
