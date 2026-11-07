package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// A day that is over keeps the price it was saved with. Pricing is worked out when it is asked for, from the settings, so without this a change of
// the price source (one rate, bills, a published rate) would change what every day in the history cost, and so would a tariff being read again.
// Each finished day is saved once, at the price it has at that moment; today and the days to come always follow the settings. The server never
// overwrites a saved day: only "re-price" (an explicit request, with the difference shown first) removes saved days so the settings price them again.
// A day with no known price is not saved, and neither is a day priced by a bill's stand-in (it is corrected the moment the bill that covers it is entered).

const (
	// freezeEvery is how often finished days are saved. A day is over at local midnight, so this is how long one can stay unsaved.
	freezeEvery = 30 * time.Minute
	// maxFreezeDays bounds one pass, so a database with a very old first minute cannot make it long.
	maxFreezeDays = 3700
	// maxRepriceDays bounds one re-price.
	maxRepriceDays = 3700
)

// frozenPrices is every saved day as the schedule wants it.
func (s *Server) frozenPrices(ctx context.Context) (map[rates.Date]rates.Money, error) {
	list, err := s.Store.DayPrices(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[rates.Date]rates.Money, len(list))
	for _, d := range list {
		day, err := rates.ParseDate(d.Day)
		if err != nil {
			continue
		}
		m, err := rates.ParseMoney(d.Rate)
		if err != nil {
			continue
		}
		out[day] = m
	}
	return out, nil
}

// freezePrices saves, for every finished day from the first recorded one that is not saved yet and has a firm price under cfg's settings, that price. It
// returns how many days it saved.
func (s *Server) freezePrices(ctx context.Context, cfg rates.Config) (int, error) {
	first, ok, err := s.Store.FirstData(ctx)
	if err != nil || !ok {
		return 0, err
	}
	today := rates.DateOf(s.now(), s.Loc)
	start := rates.DateOf(first, s.Loc)
	if today.DaysSince(start) > maxFreezeDays {
		start = today.AddDays(-maxFreezeDays)
	}
	have, err := s.frozenPrices(ctx)
	if err != nil {
		return 0, err
	}
	live := cfg.Live(s.Loc)
	var add []store.DayPrice
	for day := start; day.Before(today); day = day.AddDays(1) {
		if _, saved := have[day]; saved {
			continue
		}
		rate := live.RateAt(day)
		if rate == nil || live.IsEstimate(day) {
			continue
		}
		add = append(add, store.DayPrice{Day: day.String(), Rate: rate.String(), Source: string(cfg.Mode)})
	}
	return s.Store.SaveDayPrices(ctx, add, s.now())
}

// RunPriceFreeze saves finished days as they happen: once when the server starts and then every half hour.
func (s *Server) RunPriceFreeze(ctx context.Context, logf func(format string, a ...any)) {
	tick := time.NewTicker(freezeEvery)
	defer tick.Stop()
	for {
		cfg, err := s.savedRateConfig(ctx)
		if err == nil {
			var n int
			if n, err = s.freezePrices(ctx, cfg); n > 0 {
				logf("saved the price of %d finished day(s)", n)
			}
		}
		if err != nil && ctx.Err() == nil {
			logf("saving the price of finished days: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

type frozenAnswer struct {
	// Days is how many finished days are saved; Through the latest of them (null with none).
	Days    int     `json:"days"`
	Through *string `json:"through"`
	// From is the earliest.
	From *string `json:"from"`
	// BySource counts the saved days by where their price came from ("one", "bills", "lookup").
	BySource map[string]int `json:"by_source"`
}

// frozenSummary says which finished days keep their price, for the price settings to show.
func (s *Server) frozenSummary(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.DayPrices(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out := frozenAnswer{Days: len(list), BySource: map[string]int{}}
	for _, d := range list {
		out.BySource[d.Source]++
	}
	if len(list) > 0 {
		first, last := list[0].Day, list[len(list)-1].Day
		out.From, out.Through = &first, &last
	}
	writeJSON(w, http.StatusOK, out)
}

type repriceBody struct {
	// From and To are local calendar days, inclusive; To defaults to yesterday.
	From string  `json:"from"`
	To   *string `json:"to"`
	// Apply makes the change; without it the answer only shows what it would be.
	Apply bool `json:"apply"`
}

type repriceAnswer struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Days is how many saved days in the range would change.
	Days int `json:"days"`
	// Source is the price source they would take their price from.
	Source string `json:"source"`
	// KWh is what the house (the mains) used in the range; null with no mains meter. OldCost and NewCost are the cost of it as saved and as it would be, and
	// Difference the second less the first; all null with no price either way or no mains.
	KWh        *float64 `json:"kwh"`
	OldCost    *string  `json:"old_cost"`
	NewCost    *string  `json:"new_cost"`
	Difference *string  `json:"difference"`
	// Applied says the saved days were removed and priced again.
	Applied bool `json:"applied"`
}

// reprice shows what pricing the finished days in a range from the current settings would change, and with apply does it. It is the only thing that
// changes the price of a day that is over (besides a bill entered while prices come from bills).
func (s *Server) reprice(w http.ResponseWriter, r *http.Request) {
	var b repriceBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_reprice", `send {"from": "2026-10-01", "to": "2026-10-31", "apply": false}`)
		return
	}
	from, err := rates.ParseDate(b.From)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_reprice", err.Error())
		return
	}
	yesterday := rates.DateOf(s.now(), s.Loc).AddDays(-1)
	to := yesterday
	if b.To != nil && *b.To != "" {
		if to, err = rates.ParseDate(*b.To); err != nil {
			writeProblem(w, http.StatusBadRequest, "bad_reprice", err.Error())
			return
		}
	}
	switch {
	case to.After(yesterday):
		writeProblem(w, http.StatusBadRequest, "bad_reprice", "Only days that are over can be priced again. Today follows the settings already.")
		return
	case to.Before(from):
		writeProblem(w, http.StatusBadRequest, "bad_reprice", "The last day is before the first.")
		return
	case to.DaysSince(from) >= maxRepriceDays:
		writeProblem(w, http.StatusBadRequest, "bad_reprice", "That range is too long.")
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	live, kept := cfg.Live(s.Loc), cfg.Schedule(s.Loc)
	out := repriceAnswer{From: from.String(), To: to.String(), Source: string(cfg.Mode)}
	for day := from; !day.After(to); day = day.AddDays(1) {
		was, saved := cfg.Frozen[day]
		now := live.RateAt(day)
		if saved && (now == nil || !now.Equal(was)) {
			out.Days++
		}
	}
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	fromT, toT := s.scheduleDayStart(from), s.scheduleDayStart(to.AddDays(1))
	mains := false
	for _, m := range on {
		if m.Scope == model.ScopeMains {
			mains = true
		}
	}
	if mains {
		oldF, err := s.houseFigures(r.Context(), on, fromT, toT, kept)
		if err != nil {
			serverError(w, err)
			return
		}
		newF, err := s.houseFigures(r.Context(), on, fromT, toT, live)
		if err != nil {
			serverError(w, err)
			return
		}
		kwh := oldF.KWh
		out.KWh = &kwh
		if oldF.Cost != nil && newF.Cost != nil {
			o, n := rates.MustMoney(*oldF.Cost), rates.MustMoney(*newF.Cost)
			d := n.Sub(o).String()
			out.OldCost, out.NewCost, out.Difference = oldF.Cost, newF.Cost, &d
		}
	}
	if b.Apply {
		if _, err := s.Store.ForgetDayPrices(r.Context(), from.String(), to.String()); err != nil {
			serverError(w, err)
			return
		}
		if _, err := s.freezePrices(r.Context(), cfg); err != nil {
			serverError(w, err)
			return
		}
		out.Applied = true
	}
	writeJSON(w, http.StatusOK, out)
}
