package api

import (
	"context"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// Checking each bill against what the house recorded over the same days. The house is the mains meters added together, and nothing else; it is a check of the
// recording, so it changes no figure. The bill's own period is what the utility billed for (their meter reads, their day boundaries), so a small difference is
// expected: the check says how large it is, how much of the period was recorded at all, and which outages fall inside it.

const (
	// closeShare and matchShare are how far from the billed energy the recorded energy may be (as a share of the bill) to be called matching or close.
	matchShare = 0.03
	closeShare = 0.06
	// fullCoverage: a period recorded for at least this share of its time is called fully recorded.
	fullCoverage = 0.95
)

type reconOutages struct {
	PowerSeconds   int64 `json:"power_seconds"`
	ServerSeconds  int64 `json:"server_seconds"`
	UnknownSeconds int64 `json:"unknown_seconds"`
}

type billCheck struct {
	Start rates.Date `json:"start"`
	End   rates.Date `json:"end"`
	Days  int        `json:"days"`
	// What the bill says: the kWh it charged for, its total, the flat fees in it, and the price of a kWh that is left once they are taken out.
	BilledKWh      float64 `json:"billed_kwh"`
	BilledTotal    string  `json:"billed_total"`
	BilledFixed    string  `json:"billed_fixed"`
	BilledVariable string  `json:"billed_variable"`
	BilledPrice    string  `json:"billed_price"`
	// What was recorded over the same days. All null when the period has not started being recorded or the house has no mains meter.
	RecordedKWh *float64 `json:"recorded_kwh"`
	// Coverage is the share (0 to 1) of the period the house was recorded for.
	Coverage *float64 `json:"coverage"`
	// DiffKWh is recorded less billed; DiffShare that as a share of the bill (negative: less was recorded than billed).
	DiffKWh   *float64 `json:"diff_kwh"`
	DiffShare *float64 `json:"diff_share"`
	// RecordedCost is the recorded energy at the prices the days were saved with (null with no price), DiffCost that less the bill's variable charge, and
	// RecordedPrice the average price per kWh those days had. When the price comes from the bills this is the bill's own price, so only the energy is an independent check.
	RecordedCost  *string `json:"recorded_cost"`
	DiffCost      *string `json:"diff_cost"`
	RecordedPrice *string `json:"recorded_price"`
	// Verdict is "match", "close" or "differs" by how far the recorded energy is from the billed (null when it cannot be told).
	Verdict *string `json:"verdict"`
	// InProgress says the bill's last day has not ended yet, so the recording so far is only part of it.
	InProgress bool         `json:"in_progress"`
	Outages    reconOutages `json:"outages"`
}

type reconciliation struct {
	// PriceSource is where the price of a kWh comes from now ("one", "bills", "lookup").
	PriceSource string `json:"price_source"`
	// HaveHouse says the house has a mains meter to check bills against.
	HaveHouse bool        `json:"have_house"`
	Bills     []billCheck `json:"bills"`
	// TypicalShare is the middle of recorded over billed across the fully recorded bills (1.00 is exact, 0.98 means 2% less was recorded than billed), and
	// TypicalBills how many bills it is made from; null with fewer than two.
	TypicalShare *float64 `json:"typical_share"`
	TypicalBills int      `json:"typical_bills"`
}

// reconcileBills checks every bill against the recording, newest bill first.
func (s *Server) reconcileBills(ctx context.Context) (reconciliation, error) {
	cfg, err := s.rateConfig(ctx)
	if err != nil {
		return reconciliation{}, err
	}
	out := reconciliation{PriceSource: string(cfg.Mode), Bills: []billCheck{}}
	on, _, err := s.enabledMeters(ctx)
	if err != nil {
		return out, err
	}
	var mains []model.Meter
	for _, m := range on {
		if m.Scope == model.ScopeMains {
			mains = append(mains, m)
		}
	}
	out.HaveHouse = len(mains) > 0
	schedule := cfg.Schedule(s.Loc)
	now := s.now()
	outages, err := s.Store.Outages(ctx, time.Unix(0, 0), 1000)
	if err != nil {
		return out, err
	}
	bills := sortedBills(cfg.Bills)
	var shares []float64
	for i := len(bills) - 1; i >= 0; i-- {
		b := bills[i]
		from, to := s.scheduleDayStart(b.Start), s.scheduleDayStart(b.End.AddDays(1))
		c := billCheck{
			Start: b.Start, End: b.End, Days: b.Days(), BilledKWh: b.Kwh,
			BilledTotal: b.Total.String(), BilledFixed: b.FixedTotal().String(), BilledVariable: b.Variable().String(), BilledPrice: b.PricePerKwh().String(),
			InProgress: to.After(now),
		}
		upTo := to
		if c.InProgress {
			upTo = now
		}
		c.Outages = outagesWithin(outages, from, upTo)
		if len(mains) > 0 && upTo.After(from) {
			f, err := s.houseFigures(ctx, on, from, upTo, schedule)
			if err != nil {
				return out, err
			}
			var hours float64
			for _, m := range mains {
				t, err := s.Store.Totals(ctx, m.ID, from, upTo)
				if err != nil {
					return out, err
				}
				hours += t.MonitoredHours
			}
			cov := math.Min(1, hours/float64(len(mains))/upTo.Sub(from).Hours())
			// Nothing recorded in the period at all: there is nothing to compare.
			if hours > 0 {
				kwh := f.KWh
				c.RecordedKWh, c.Coverage = &kwh, &cov
				if !c.InProgress {
					diff := kwh - b.Kwh
					share := diff / b.Kwh
					c.DiffKWh, c.DiffShare = &diff, &share
					v := verdictOf(share)
					c.Verdict = &v
					if cov >= fullCoverage {
						shares = append(shares, kwh/b.Kwh)
					}
				}
				if f.Cost != nil && kwh > 0 {
					cost := rates.MustMoney(*f.Cost)
					cs := cost.String()
					c.RecordedCost = &cs
					price := cost.Div(rates.MoneyFromFloat(kwh)).String()
					c.RecordedPrice = &price
					if !c.InProgress {
						d := cost.Sub(b.Variable()).String()
						c.DiffCost = &d
					}
				}
			}
		}
		out.Bills = append(out.Bills, c)
	}
	if len(shares) >= 2 {
		sort.Float64s(shares)
		mid := shares[len(shares)/2]
		if len(shares)%2 == 0 {
			mid = (shares[len(shares)/2-1] + shares[len(shares)/2]) / 2
		}
		out.TypicalShare, out.TypicalBills = &mid, len(shares)
	}
	return out, nil
}

func verdictOf(share float64) string {
	a := math.Abs(share)
	switch {
	case a <= matchShare:
		return "match"
	case a <= closeShare:
		return "close"
	}
	return "differs"
}

// outagesWithin adds up the seconds of each kind of outage that fall inside [from, to).
func outagesWithin(list []store.Outage, from, to time.Time) reconOutages {
	var o reconOutages
	for _, x := range list {
		start, end := x.Started, x.Ended
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if !end.After(start) {
			continue
		}
		secs := int64(end.Sub(start).Seconds())
		switch x.Kind {
		case store.OutagePower:
			o.PowerSeconds += secs
		case store.OutageServer:
			o.ServerSeconds += secs
		default:
			o.UnknownSeconds += secs
		}
	}
	return o
}

func (s *Server) billReconciliation(w http.ResponseWriter, r *http.Request) {
	out, err := s.reconcileBills(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
