package api

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// The house is the mains clamps added together, and nothing else: circuits and plugs are parts of it and are never added to it. These are the
// whole-house figures the main dashboard shows, the same kinds of figure a computer's page has (energy, cost, highest draw, carbon, an estimate,
// a history list and a CSV).

// houseFigures adds up the mains meters over [from, to). Have is false when no mains meter is shown.
type houseFigures struct {
	Have      bool
	KWh       float64
	Cost      *string
	Estimated bool
	// PeakWatts is the mains' highest draws added: an upper bound, as the two legs need not peak in the same second.
	PeakWatts *float64
	CarbonG   *float64
	CarbonEst *float64
}

func (s *Server) houseFigures(ctx context.Context, on []model.Meter, from, to time.Time, schedule *rates.Schedule) (houseFigures, error) {
	var h houseFigures
	var cost rates.Money
	priced := true
	var peak, carbonG, carbonEst float64
	havePeak, haveCarbon := false, false
	for _, ch := range on {
		if ch.Scope != model.ScopeMains {
			continue
		}
		cs, pc, err := s.meterSummary(ctx, ch, from, to, schedule)
		if err != nil {
			return h, err
		}
		h.Have = true
		h.KWh += cs.KWh
		if pc.Unpriced {
			priced = false
		} else {
			cost = cost.Add(pc.Cost)
		}
		h.Estimated = h.Estimated || (cs.Cost != nil && pc.Estimated)
		if cs.PeakWatts != nil {
			peak += *cs.PeakWatts
			havePeak = true
		}
		if cs.CarbonG != nil {
			carbonG += *cs.CarbonG
			haveCarbon = true
			if cs.CarbonEstimatedKWh != nil {
				carbonEst += *cs.CarbonEstimatedKWh
			}
		}
	}
	if !h.Have {
		return h, nil
	}
	if priced {
		c := cost.String()
		h.Cost = &c
	}
	if havePeak {
		h.PeakWatts = &peak
	}
	if haveCarbon {
		h.CarbonG, h.CarbonEst = &carbonG, &carbonEst
	}
	return h, nil
}

// houseHistory is one row per rolling period: the whole house's energy, what it cost, its highest draw and its carbon.
func (s *Server) houseHistory(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	to := s.now().Truncate(time.Minute).Add(time.Minute)
	out := model.HouseHistory{Periods: []model.HouseHistoryPeriod{}}
	for _, p := range historyPeriods {
		from := allTimeFrom
		if p.span > 0 {
			from = to.Add(-p.span)
		}
		f, err := s.houseFigures(r.Context(), on, from, to, schedule)
		if err != nil {
			serverError(w, err)
			return
		}
		if !f.Have {
			break
		}
		out.Periods = append(out.Periods, model.HouseHistoryPeriod{
			Key: p.key, Label: p.label, From: from.UTC(), To: to.UTC(), KWh: f.KWh, Cost: f.Cost, CostEstimated: f.Cost != nil && f.Estimated,
			PeakWatts: f.PeakWatts, CarbonG: f.CarbonG, CarbonEstimatedKWh: f.CarbonEst,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// houseEstimate gives the cost estimator today's price and what the house really used per day over a window ("24h", "7d", "30d", "all";
// default 7d), counting every hour, averaged over the time actually recorded; usage is null when less than an hour has been.
func (s *Server) houseEstimate(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	now := s.now()
	today := rates.DateOf(now, s.Loc)
	out := model.ComputerEstimate{Currency: cfg.Currency, RateEstimated: schedule.IsEstimate(today)}
	if rate := schedule.RateAt(today); rate != nil {
		c := rate.String()
		out.Rate = &c
	}
	spans := map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "all": 0}
	key := r.URL.Query().Get("window")
	if key == "" {
		key = "7d"
	}
	span, ok := spans[key]
	if !ok {
		writeProblem(w, http.StatusBadRequest, "bad_window", "window is 24h, 7d, 30d or all")
		return
	}
	from := allTimeFrom
	if span > 0 {
		from = now.Add(-span)
	}
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var mains []model.Meter
	var first time.Time
	for _, ch := range on {
		if ch.Scope != model.ScopeMains {
			continue
		}
		mains = append(mains, ch)
		ft, ok, err := s.Store.FirstMinute(r.Context(), ch.ID)
		if err != nil {
			serverError(w, err)
			return
		}
		if ok && (first.IsZero() || ft.Before(first)) {
			first = ft
		}
	}
	if len(mains) == 0 || first.IsZero() {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if first.After(from) {
		from = first
	}
	if now.Sub(from) < minimumForAverage {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var kwh float64
	for _, ch := range mains {
		t, err := s.Store.Totals(r.Context(), ch.ID, from, now)
		if err != nil {
			serverError(w, err)
			return
		}
		kwh += t.KWh
	}
	days := now.Sub(from).Hours() / 24
	out.Usage = &model.AverageUse{KWh: kwh, Days: days, KWhPerDay: kwh / days, AverageWatts: kwh / days * 1000 / 24, WholePC: true, IncludesPC: false, From: from.UTC()}
	writeJSON(w, http.StatusOK, out)
}

// houseExport is the house's daily energy and cost over a period as a CSV download: one row per local day, the house (the mains), each circuit
// on its own, the day's cost of the house at that day's price, and its estimated CO2 once any grid intensity has been saved.
func (s *Server) houseExport(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	perDay := func(id string) (map[rates.Date]float64, error) {
		points, err := s.Store.Series(r.Context(), id, from, to, model.BucketDay, s.Loc)
		out := map[rates.Date]float64{}
		for _, p := range points {
			out[rates.DateOf(p.T, s.Loc)] += p.KWh
		}
		return out, err
	}
	house := map[rates.Date]float64{}
	var mainsIDs, circuitLabels []string
	var circuits []map[rates.Date]float64
	for _, ch := range on {
		switch ch.Scope {
		case model.ScopeMains:
			m, err := perDay(ch.ID)
			if err != nil {
				serverError(w, err)
				return
			}
			for d, k := range m {
				house[d] += k
			}
			mainsIDs = append(mainsIDs, ch.ID)
		case model.ScopeCircuit:
			m, err := perDay(ch.ID)
			if err != nil {
				serverError(w, err)
				return
			}
			circuitLabels, circuits = append(circuitLabels, ch.Label), append(circuits, m)
		}
	}
	ever, haveCarbon, err := s.Store.AverageCarbonEver(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var known map[int64]float64
	if haveCarbon {
		if known, err = s.Store.QueryCarbon(r.Context(), from, to); err != nil {
			serverError(w, err)
			return
		}
	}

	header := []string{"date", "house_kwh"}
	for _, l := range circuitLabels {
		header = append(header, l+" (kwh)")
	}
	header = append(header, "house_cost")
	if haveCarbon {
		header = append(header, "house_co2_kg")
	}
	var b []byte
	b = append(b, csvLine(header)...)
	last := rates.DateOf(to.Add(-time.Nanosecond), s.Loc)
	for day := rates.DateOf(from, s.Loc); !day.After(last); day = day.AddDays(1) {
		row := []string{day.String(), number(house[day])}
		for _, c := range circuits {
			row = append(row, number(c[day]))
		}
		cost := ""
		if rate := schedule.RateAt(day); rate != nil {
			cost = rates.MoneyFromFloat(house[day]).Mul(*rate).Fixed(4)
		}
		row = append(row, cost)
		if haveCarbon {
			f, t := s.dayBounds(day, from, to)
			hours, err := s.Store.HourlyKWh(r.Context(), mainsIDs, f, t)
			if err != nil {
				serverError(w, err)
				return
			}
			row = append(row, number(carbonResult(known, ever, hours).Kg))
		}
		b = append(b, csvLine(row)...)
	}
	name := "kvguzzler-house-daily-" + rates.DateOf(from, s.Loc).String() + "-to-" + last.String() + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(name)+`"`)
	_, _ = w.Write(b)
}
