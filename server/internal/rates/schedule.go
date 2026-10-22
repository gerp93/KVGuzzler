package rates

import (
	"sort"
	"time"
)

type Mode string

const (
	// ModeOne is one price per kWh, used for everything.
	ModeOne Mode = "one"
	// ModeBills is each bill's price for the dates it covers.
	ModeBills Mode = "bills"
	// ModeLookup is a utility's published tariff. It is not built in the server yet, so a schedule in this mode
	// prices with the single rate, as the desktop app does when it has no tariff.
	ModeLookup Mode = "lookup"
)

// Segment is a stretch of time with one price. Rate is nil when no price is known at all.
type Segment struct {
	From, To time.Time
	Rate     *Money
	Estimate bool
}

// RatedCost is the cost of some energy, how much energy there was, and whether any part was priced by estimate or not priced.
type RatedCost struct {
	Cost      Money
	Kwh       float64
	Estimated bool
	Unpriced  bool
}

// Schedule answers "what did a kWh cost on this day?". With one rate that is always the same. With bills, a day inside a
// bill has that bill's price; a day after the latest bill, or in a gap between bills, uses the most recent bill's price as
// an estimate (corrected the moment the next bill is entered); a day before the first bill uses the single rate, or the
// first bill's price if there is none.
type Schedule struct {
	mode  Mode
	flat  *Money
	bills []Bill
	loc   *time.Location
}

func NewSchedule(mode Mode, flat *Money, bills []Bill, loc *time.Location) *Schedule {
	sorted := append([]Bill(nil), bills...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	if loc == nil {
		loc = time.Local
	}
	return &Schedule{mode: mode, flat: flat, bills: sorted, loc: loc}
}

func (s *Schedule) usesBills() bool { return s.mode == ModeBills && len(s.bills) > 0 }

func (s *Schedule) covering(day Date) *Bill {
	for i := range s.bills {
		if !s.bills[i].Start.After(day) && !day.After(s.bills[i].End) {
			return &s.bills[i]
		}
	}
	return nil
}

// RateAt is the price of a kWh on a local day, or nil when nothing is known.
func (s *Schedule) RateAt(day Date) *Money {
	if !s.usesBills() {
		return s.flat
	}
	if b := s.covering(day); b != nil {
		p := b.PricePerKwh()
		return &p
	}
	for i := len(s.bills) - 1; i >= 0; i-- {
		if s.bills[i].End.Before(day) {
			p := s.bills[i].PricePerKwh()
			return &p
		}
	}
	if s.flat != nil {
		return s.flat // a day before the first bill
	}
	p := s.bills[0].PricePerKwh()
	return &p
}

// IsEstimate is true when the price for this day is a stand-in rather than the price of a bill that covers it.
func (s *Schedule) IsEstimate(day Date) bool { return s.usesBills() && s.covering(day) == nil }

// StartOf is the instant a local calendar day begins.
func (s *Schedule) StartOf(day Date) time.Time {
	return time.Date(day.Year, day.Month, day.Day, 0, 0, 0, 0, s.loc)
}

func sameRate(a, b *Money) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// Segments splits a period at local midnights into stretches of one price each (neighbours with the same price are joined).
func (s *Schedule) Segments(from, to time.Time) []Segment {
	var out []Segment
	if !to.After(from) {
		return out
	}
	for day := DateOf(from, s.loc); s.StartOf(day).Before(to); day = day.AddDays(1) {
		start := s.StartOf(day)
		if start.Before(from) {
			start = from
		}
		end := s.StartOf(day.AddDays(1))
		if end.After(to) {
			end = to
		}
		if !end.After(start) {
			continue
		}
		rate, estimate := s.RateAt(day), s.IsEstimate(day)
		if n := len(out); n > 0 && sameRate(out[n-1].Rate, rate) && out[n-1].Estimate == estimate {
			out[n-1].To = end
		} else {
			out = append(out, Segment{From: start, To: end, Rate: rate, Estimate: estimate})
		}
	}
	return out
}

// Cost prices the energy in a period: each stretch's energy times that stretch's price. kwhBetween is the energy used
// between two instants (read from the history).
func (s *Schedule) Cost(kwhBetween func(from, to time.Time) float64, from, to time.Time) RatedCost {
	var rc RatedCost
	for _, seg := range s.Segments(from, to) {
		energy := kwhBetween(seg.From, seg.To)
		rc.Kwh += energy
		if seg.Rate == nil {
			if energy > 0 {
				rc.Unpriced = true
			}
			continue
		}
		rc.Cost = rc.Cost.Add(priceOf(energy, *seg.Rate))
		if seg.Estimate && energy > 0 {
			rc.Estimated = true
		}
	}
	return rc
}

func priceOf(kwh float64, perKwh Money) Money { return MoneyFromFloat(kwh).Mul(perKwh) }

// AverageRate is the price over a period weighted by time, for pricing many small amounts with one number.
func (s *Schedule) AverageRate(from, to time.Time) *Money {
	var weighted, seconds Money
	for _, seg := range s.Segments(from, to) {
		if seg.Rate == nil {
			continue
		}
		length := MoneyFromFloat(seg.To.Sub(seg.From).Seconds())
		weighted = weighted.Add(seg.Rate.Mul(length))
		seconds = seconds.Add(length)
	}
	if seconds.Sign() > 0 {
		avg := weighted.Div(seconds)
		return &avg
	}
	return s.RateAt(DateOf(to, s.loc))
}

// Span is the instants a bill's dates cover: from the start of its first day to the end of its last.
func (s *Schedule) Span(b Bill) (from, to time.Time) {
	return s.StartOf(b.Start), s.StartOf(b.End.AddDays(1))
}
