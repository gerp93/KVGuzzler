package api

import (
	"context"
	"sort"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// Banked miles. Charging buys energy before the miles that use it are driven, so counting the energy of a stretch between two odometer readings against
// that stretch's miles makes a stretch with a charge and no driving look expensive, and the stretch that spends it look free. What the car's battery held
// at the two ends says which part of the energy charged is still unspent: it is taken out of the stretch, and what is left is what the miles used. The
// battery's charge is only known at the moments the car reported it (the Ford feed's looks and the start and the end of each charge), so the charge at a
// day's start is worked out between the reports on either side.
const (
	// socMaxGap is the longest stretch between two reports a charge is worked out across while the battery moved: a car that was driven or charged in
	// that time was doing it at a pace nobody knows.
	socMaxGap = 12 * time.Hour
	// socIdle is how little (in percentage points) the two reports may differ for the battery to count as not having moved, so that a car left parked
	// can be worked out across any gap.
	socIdle = 1.0
)

// socPoint is one report of the battery's charge.
type socPoint struct {
	At  time.Time
	Pct float64
}

// evSocPoints is every report of a meter's battery charge: the looks the feed made and the charges' own first and last reports, oldest first.
func (s *Server) evSocPoints(ctx context.Context, meterID string) ([]socPoint, error) {
	reads, err := s.Store.EvBatteryReads(ctx, meterID)
	if err != nil {
		return nil, err
	}
	charges, err := s.Store.EvCharges(ctx, meterID, maxEvCharges)
	if err != nil {
		return nil, err
	}
	return socPointsOf(reads, charges), nil
}

func socPointsOf(reads []model.EvBatteryRead, charges []model.EvCharge) []socPoint {
	var out []socPoint
	for _, r := range reads {
		out = append(out, socPoint{r.At, r.Percent})
	}
	for _, c := range charges {
		if c.StartPct != nil && c.StartPctAt != nil {
			out = append(out, socPoint{*c.StartPctAt, *c.StartPct})
		}
		if c.EndPct != nil && c.EndPctAt != nil {
			out = append(out, socPoint{*c.EndPctAt, *c.EndPct})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	// The same moment reported twice (a look and a charge's report of it) is one report.
	dedup := out[:0]
	for _, p := range out {
		if n := len(dedup); n > 0 && dedup[n-1].At.Equal(p.At) {
			continue
		}
		dedup = append(dedup, p)
	}
	return dedup
}

// socAt is the battery's charge at t, worked out between the reports on either side of it (a straight line in time). False when it cannot be said: t is
// before the first report, after the last by more than socMaxGap, or between two reports that are far apart while the battery moved.
func socAt(points []socPoint, t time.Time) (float64, bool) {
	i := sort.Search(len(points), func(i int) bool { return points[i].At.After(t) })
	switch {
	case len(points) == 0:
		return 0, false
	case i == 0:
		if points[0].At.Equal(t) {
			return points[0].Pct, true
		}
		return 0, false
	case i == len(points):
		last := points[len(points)-1]
		return last.Pct, t.Sub(last.At) <= socMaxGap
	}
	a, b := points[i-1], points[i]
	gap := b.At.Sub(a.At)
	if gap > socMaxGap && abs(b.Pct-a.Pct) > socIdle {
		return 0, false
	}
	frac := float64(t.Sub(a.At)) / float64(gap)
	return a.Pct + (b.Pct-a.Pct)*frac, true
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// wallKWhPerPct is the energy at the wall that each percent of the battery has taken over the charges that have one (a bigger charge counts for more),
// and what that energy cost per kWh over the same charges (nil when a charge had no price). False when no charge has an energy per percent.
func (s *Server) wallKWhPerPct(ctx context.Context, m model.Meter) (float64, *rates.Money, bool, error) {
	list, err := s.Store.EvCharges(ctx, m.ID, maxEvCharges)
	if err != nil {
		return 0, nil, false, err
	}
	now := s.now()
	var kwh, gain float64
	var cost rates.Money
	priced := true
	for _, c := range list {
		v, err := s.evChargeView(ctx, m, c, now)
		if err != nil {
			return 0, nil, false, err
		}
		if v.KWhPerPct == nil {
			continue
		}
		kwh += v.KWh
		gain += *v.GainPct
		if v.Cost == nil {
			priced = false
		} else if p, err := rates.ParseMoney(*v.Cost); err == nil {
			cost = cost.Add(p)
		} else {
			priced = false
		}
	}
	if gain <= 0 || kwh <= 0 {
		return 0, nil, false, nil
	}
	var price *rates.Money
	if priced {
		p := cost.Div(rates.MoneyFromFloat(kwh))
		price = &p
	}
	return kwh / gain, price, true, nil
}
