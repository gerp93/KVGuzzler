package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/gasprice"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// What driving the same miles in a gasoline car would have cost, set beside what the meter's energy cost for the electric one. Not one gas car but several at
// once: the presets below, and optionally one the owner names. The owner says which area's price to use; the weekly prices are EIA's, read with the key saved
// for the carbon footprint. Which comparisons are drawn is the dashboard's choice (each viewer's own), so the server answers all of them.
const (
	evCompareKey   = "ev_compare:" // + the meter's id
	gasStatusKey   = "gas_status:" // + the area's code
	gasHistory     = 3 * 365 * 24 * time.Hour
	gasFeedEvery   = 12 * time.Hour
	gasFeedFirst   = time.Minute
	minMPG, maxMPG = 5.0, 200.0
	// pricesBeforeFirstReading is how much of the price history before the first odometer reading is listed for the chart.
	pricesBeforeFirstReading = 14 * 24 * time.Hour
	// pricesWithoutReadings is how far back the chart goes when there are no readings yet.
	pricesWithoutReadings = 26 * 7 * 24 * time.Hour
)

// evMPGPresets are the gas cars every electric vehicle is compared with, in miles per gallon: from a thirsty truck to an economy car.
var evMPGPresets = []float64{15, 20, 25, 30, 35, 40}

// evCompareSetup is what is saved for a charging meter: the price area and, if the owner named one, a gas car of their own.
type evCompareSetup struct {
	AreaID    string   `json:"area_id"`
	CustomMPG *float64 `json:"custom_mpg"`
	// MPG is what a setup saved before there were presets said (the one gas car compared with). It is read, never written: a figure that is not a preset
	// becomes the custom one, and one that is a preset is already there.
	MPG *float64 `json:"mpg,omitempty"`
}

// evCompareBody is what the owner sends to set it up.
type evCompareBody struct {
	AreaID    string   `json:"area_id"`
	CustomMPG *float64 `json:"custom_mpg"`
}

type gasStatus struct {
	CheckedAt time.Time `json:"checked_at"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
}

type evCompareView struct {
	MeterID  string  `json:"meter_id"`
	AreaID   *string `json:"area_id"`
	AreaName *string `json:"area_name"`
	// CustomMPG is the owner's own gas car, beside the presets; null when there is none.
	CustomMPG *float64        `json:"custom_mpg"`
	Presets   []float64       `json:"presets"`
	Areas     []gasprice.Area `json:"areas"`
	// KeySaved says the EIA key (saved under Carbon) that the prices need is there.
	KeySaved    bool       `json:"key_saved"`
	LastCheck   *time.Time `json:"last_check"`
	LastOutcome *string    `json:"last_outcome"`
	LatestWeek  *string    `json:"latest_week"`
	LatestPrice *string    `json:"latest_price"`
}

// evComparison is one gas car the electric vehicle is compared with.
type evComparison struct {
	MPG    float64 `json:"mpg"`
	Custom bool    `json:"custom"`
}

// evSavingsGas is what a stretch's miles would have cost in one gas car.
type evSavingsGas struct {
	MPG float64 `json:"mpg"`
	// Gallons is what the gas car would have burned over the miles.
	Gallons float64 `json:"gallons"`
	GasCost *string `json:"gas_cost"`
	// Savings is the gas car's cost less the electric car's: negative when the electric car cost more. Null when either cost is unknown.
	Savings *string `json:"savings"`
}

type evSavingsStretch struct {
	FromDay string  `json:"from_day"`
	ToDay   string  `json:"to_day"`
	Miles   float64 `json:"miles"`
	// GasPrice is the average of the week prices over the stretch's days; null when the days are not all covered by a price.
	GasPrice *string `json:"gas_price"`
	EvCost   *string `json:"ev_cost"`
	// Gas has one entry for each of the answer's comparisons, in the same order.
	Gas []evSavingsGas `json:"gas"`
}

// evTotalsGas is one gas car's cost over the stretches that were counted.
type evTotalsGas struct {
	MPG            float64 `json:"mpg"`
	Gallons        float64 `json:"gallons"`
	GasCost        string  `json:"gas_cost"`
	Savings        string  `json:"savings"`
	GasCostPerMile *string `json:"gas_cost_per_mile"`
}

type evSavingsTotals struct {
	// Stretches is how many stretches are added up: only those with both costs known, the same for every gas car.
	Stretches     int     `json:"stretches"`
	Miles         float64 `json:"miles"`
	EvCost        string  `json:"ev_cost"`
	EvCostPerMile *string `json:"ev_cost_per_mile"`
	// Gas has one entry for each of the answer's comparisons, in the same order.
	Gas []evTotalsGas `json:"gas"`
}

type evSavingsAnswer struct {
	MeterID    string  `json:"meter_id"`
	Configured bool    `json:"configured"`
	AreaName   *string `json:"area_name"`
	// Comparisons are the gas cars compared with, by miles per gallon, lowest first: the presets and the owner's own if there is one.
	Comparisons   []evComparison     `json:"comparisons"`
	Prices        []gasprice.Price   `json:"prices"`
	Stretches     []evSavingsStretch `json:"stretches"`
	Totals        *evSavingsTotals   `json:"totals"`
	CostEstimated bool               `json:"cost_estimated"`
}

// evComparisons are the gas cars to compare with: the presets, and the custom one in its place among them.
func evComparisons(c *evCompareSetup) []evComparison {
	out := make([]evComparison, 0, len(evMPGPresets)+1)
	for _, p := range evMPGPresets {
		out = append(out, evComparison{MPG: p})
	}
	if c != nil && c.CustomMPG != nil {
		out = append(out, evComparison{MPG: *c.CustomMPG, Custom: true})
		sort.SliceStable(out, func(i, j int) bool { return out[i].MPG < out[j].MPG })
	}
	return out
}

func isPresetMPG(mpg float64) bool {
	for _, p := range evMPGPresets {
		if p == mpg {
			return true
		}
	}
	return false
}

func (s *Server) gasClient() *gasprice.Client {
	if s.Gas != nil {
		return s.Gas
	}
	return &gasprice.Client{}
}

func (s *Server) evCompareSetup(ctx context.Context, meterID string) (*evCompareSetup, error) {
	text, ok, err := s.Store.Meta(ctx, evCompareKey+meterID)
	if err != nil || !ok || text == "" {
		return nil, err
	}
	var c evCompareSetup
	if json.Unmarshal([]byte(text), &c) != nil {
		return nil, nil
	}
	if _, ok := gasprice.AreaName(c.AreaID); !ok {
		return nil, nil
	}
	// The one gas car an older setup named becomes the custom one unless it is a preset.
	if c.CustomMPG == nil && c.MPG != nil {
		c.CustomMPG = c.MPG
	}
	c.MPG = nil
	if c.CustomMPG != nil && (!(*c.CustomMPG >= minMPG && *c.CustomMPG <= maxMPG) || isPresetMPG(*c.CustomMPG)) {
		c.CustomMPG = nil
	}
	return &c, nil
}

func (s *Server) gasStatus(ctx context.Context, area string) (*gasStatus, error) {
	text, ok, err := s.Store.Meta(ctx, gasStatusKey+area)
	if err != nil || !ok {
		return nil, err
	}
	var st gasStatus
	if json.Unmarshal([]byte(text), &st) != nil {
		return nil, nil
	}
	return &st, nil
}

// refreshGas reads an area's weekly prices from EIA and keeps them, and keeps what happened. The error is for the server's own trouble; EIA's complaints are
// in the status.
func (s *Server) refreshGas(ctx context.Context, area string) error {
	st := gasStatus{CheckedAt: s.now().UTC()}
	cfg, err := s.LoadCarbonConfig(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		st.Error = "The gas prices come from EIA and need its key: save it under Carbon first."
	} else {
		prices, err := s.gasClient().Weekly(ctx, cfg.APIKey, area, s.now().Add(-gasHistory))
		var ge *gasprice.Error
		switch {
		case err == nil:
			if err := s.Store.SaveGasPrices(ctx, area, prices); err != nil {
				return err
			}
			st.OK = true
		case errors.As(err, &ge):
			st.Error = ge.Message
		default:
			return err // not EIA's complaint (the request was cancelled)
		}
	}
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.Store.SetMeta(ctx, gasStatusKey+area, string(body))
}

// RunGasFeed keeps the prices of every area an electric vehicle is compared in up to date, until ctx ends. EIA publishes a week's price once a week, so
// twice a day is plenty. A failure is reported with logf and never stops it: an optional feature must not stop the server.
func (s *Server) RunGasFeed(ctx context.Context, logf func(format string, a ...any)) {
	wait := gasFeedFirst
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = gasFeedEvery
		ids, err := s.Store.EVMeterIDs(ctx)
		if err != nil {
			logf("gas prices: %v", err)
			continue
		}
		done := map[string]bool{}
		for _, id := range ids {
			c, err := s.evCompareSetup(ctx, id)
			if err != nil || c == nil || done[c.AreaID] {
				continue
			}
			done[c.AreaID] = true
			if err := s.refreshGas(ctx, c.AreaID); err != nil && ctx.Err() == nil {
				logf("gas prices (%s): %v", c.AreaID, err)
			}
		}
	}
}

func (s *Server) evCompareView(ctx context.Context, meterID string) (evCompareView, error) {
	out := evCompareView{MeterID: meterID, Areas: gasprice.Areas, Presets: evMPGPresets}
	cfg, err := s.LoadCarbonConfig(ctx)
	if err != nil {
		return out, err
	}
	out.KeySaved = cfg != nil
	c, err := s.evCompareSetup(ctx, meterID)
	if err != nil || c == nil {
		return out, err
	}
	name, _ := gasprice.AreaName(c.AreaID)
	out.AreaID, out.AreaName, out.CustomMPG = &c.AreaID, &name, c.CustomMPG
	if st, err := s.gasStatus(ctx, c.AreaID); err != nil {
		return out, err
	} else if st != nil {
		at := st.CheckedAt
		out.LastCheck = &at
		msg := "Prices saved."
		if !st.OK {
			msg = st.Error
		}
		out.LastOutcome = &msg
	}
	prices, err := s.Store.GasPrices(ctx, c.AreaID)
	if err != nil {
		return out, err
	}
	if n := len(prices); n > 0 {
		out.LatestWeek, out.LatestPrice = &prices[n-1].Week, &prices[n-1].Dollars
	}
	return out, nil
}

// evMeterFor answers 404 or 409 itself and returns false when the path's meter is not one that charges an electric vehicle.
func (s *Server) evMeterFor(w http.ResponseWriter, r *http.Request) (string, bool) {
	m, err := s.Store.Meter(r.Context(), r.PathValue("meterId"))
	if err != nil {
		evStatus(w, err)
		return "", false
	}
	if !m.EV {
		writeProblem(w, http.StatusConflict, "not_ev", "this meter is not marked as charging an electric vehicle")
		return "", false
	}
	return m.ID, true
}

func (s *Server) getEvCompare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.evMeterFor(w, r)
	if !ok {
		return
	}
	v, err := s.evCompareView(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// putEvCompare saves the price area and the owner's own gas car (optional), and reads the area's prices at once. The setup is kept even when EIA cannot be
// reached or the key is not saved yet: the answer says what happened, and the feed tries again.
func (s *Server) putEvCompare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.evMeterFor(w, r)
	if !ok {
		return
	}
	var body evCompareBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_compare", `send {"area_id": "SMN", "custom_mpg": number or null}`)
		return
	}
	if body.CustomMPG != nil && (math.IsNaN(*body.CustomMPG) || *body.CustomMPG < minMPG || *body.CustomMPG > maxMPG) {
		writeProblem(w, http.StatusBadRequest, "bad_compare", "A gas car's miles per gallon is a number between 5 and 200.")
		return
	}
	if _, ok := gasprice.AreaName(body.AreaID); !ok {
		writeProblem(w, http.StatusBadRequest, "bad_compare", "Pick one of the areas EIA publishes a gasoline price for.")
		return
	}
	text, err := json.Marshal(evCompareSetup{AreaID: body.AreaID, CustomMPG: body.CustomMPG})
	if err != nil {
		serverError(w, err)
		return
	}
	if err := s.Store.SetMeta(r.Context(), evCompareKey+id, string(text)); err != nil {
		serverError(w, err)
		return
	}
	if err := s.refreshGas(r.Context(), body.AreaID); err != nil {
		serverError(w, err)
		return
	}
	s.getEvCompare(w, r)
}

func (s *Server) deleteEvCompare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.evMeterFor(w, r)
	if !ok {
		return
	}
	if err := s.Store.SetMeta(r.Context(), evCompareKey+id, ""); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// priceOn is the week price in force on a local day: the latest week on or before it. False before the first week kept. A day after the latest week keeps
// the latest price (the week's price is published on its own schedule).
func priceOn(prices []gasprice.Price, day string) (rates.Money, bool) {
	var found *gasprice.Price
	for i := range prices {
		if prices[i].Week > day {
			break
		}
		found = &prices[i]
	}
	if found == nil {
		return rates.Money{}, false
	}
	m, err := rates.ParseMoney(found.Dollars)
	return m, err == nil
}

// averagePrice is the mean of the week prices over the days from `from` up to (not including) `to`; false when a day has no price.
func averagePrice(prices []gasprice.Price, from, to time.Time) (rates.Money, bool) {
	var sum rates.Money
	n := 0
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		p, ok := priceOn(prices, d.Format("2006-01-02"))
		if !ok {
			return rates.Money{}, false
		}
		sum, n = sum.Add(p), n+1
	}
	if n == 0 {
		return rates.Money{}, false
	}
	return sum.Div(rates.MoneyFromFloat(float64(n))), true
}

func (s *Server) getEvSavings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := s.evMeterFor(w, r)
	if !ok {
		return
	}
	out := evSavingsAnswer{MeterID: id, Comparisons: []evComparison{}, Prices: []gasprice.Price{}, Stretches: []evSavingsStretch{}}
	c, err := s.evCompareSetup(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	if c == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	name, _ := gasprice.AreaName(c.AreaID)
	comps := evComparisons(c)
	out.Configured, out.AreaName, out.Comparisons = true, &name, comps
	prices, err := s.Store.GasPrices(ctx, c.AreaID)
	if err != nil {
		serverError(w, err)
		return
	}
	ev, err := s.evAnswer(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	out.CostEstimated = ev.CostEstimated

	var miles float64
	var evTotal rates.Money
	gasTotals := make([]rates.Money, len(comps))
	gallonsTotals := make([]float64, len(comps))
	counted := 0
	for _, iv := range ev.Intervals {
		from, err1 := time.Parse("2006-01-02", iv.FromDay)
		to, err2 := time.Parse("2006-01-02", iv.ToDay)
		if err1 != nil || err2 != nil {
			continue
		}
		st := evSavingsStretch{FromDay: iv.FromDay, ToDay: iv.ToDay, Miles: iv.Miles, EvCost: iv.Cost, Gas: make([]evSavingsGas, len(comps))}
		avg, haveGas := averagePrice(prices, from, to)
		if haveGas {
			p := avg.String()
			st.GasPrice = &p
		}
		var evc rates.Money
		haveEv := false
		if iv.Cost != nil {
			if m, err := rates.ParseMoney(*iv.Cost); err == nil {
				evc, haveEv = m, true
			}
		}
		for j, comp := range comps {
			g := evSavingsGas{MPG: comp.MPG, Gallons: iv.Miles / comp.MPG}
			if haveGas {
				gas := rates.MoneyFromFloat(g.Gallons).Mul(avg)
				gc := gas.String()
				g.GasCost = &gc
				if haveEv {
					sv := gas.Sub(evc).String()
					g.Savings = &sv
					gasTotals[j] = gasTotals[j].Add(gas)
					gallonsTotals[j] += g.Gallons
				}
			}
			st.Gas[j] = g
		}
		if haveGas && haveEv {
			evTotal = evTotal.Add(evc)
			miles += st.Miles
			counted++
		}
		out.Stretches = append(out.Stretches, st)
	}
	if counted > 0 {
		t := evSavingsTotals{Stretches: counted, Miles: miles, EvCost: evTotal.String(), Gas: make([]evTotalsGas, len(comps))}
		if miles > 0 {
			e := evTotal.Div(rates.MoneyFromFloat(miles)).String()
			t.EvCostPerMile = &e
		}
		for j, comp := range comps {
			g := evTotalsGas{MPG: comp.MPG, Gallons: gallonsTotals[j], GasCost: gasTotals[j].String(), Savings: gasTotals[j].Sub(evTotal).String()}
			if miles > 0 {
				pm := gasTotals[j].Div(rates.MoneyFromFloat(miles)).String()
				g.GasCostPerMile = &pm
			}
			t.Gas[j] = g
		}
		out.Totals = &t
	}

	// The prices for the chart: from a little before the first reading, or the last half year when there are none.
	since := s.now().Add(-pricesWithoutReadings).Format("2006-01-02")
	if len(ev.Readings) > 0 {
		if first, err := time.Parse("2006-01-02", ev.Readings[0].Day); err == nil {
			since = first.Add(-pricesBeforeFirstReading).Format("2006-01-02")
		}
	}
	for _, p := range prices {
		if p.Week >= since {
			out.Prices = append(out.Prices, p)
		}
	}
	writeJSON(w, http.StatusOK, out)
}
