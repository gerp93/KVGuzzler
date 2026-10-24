// Package api serves the house API described in contract/openapi.yaml.
//
// Implemented so far: status, devices (read, add, remove), meters (read, rename, switch off), live, series, summary
// (with cost from the saved rates) and the rates settings. The rest of the contract (carbon, the PC ingest) answers 501
// until it is built.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/carbon"
	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/devices"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/openei"
	"github.com/gerp93/KVGuzzler/server/internal/pcrelay"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// Version is the contract's major version this server speaks.
const Version = 1

// DeviceManager adds and removes the devices the server polls (see package devices). Nil means adding and removing answer 501.
type DeviceManager interface {
	Add(ctx context.Context, nd devices.NewDevice) (model.Device, error)
	Update(ctx context.Context, id string, u devices.UpdateDevice) (model.Device, error)
	Remove(ctx context.Context, id string) error
}

type Server struct {
	Store     *store.Store
	Collector *collector.Collector
	Devices   DeviceManager
	Loc       *time.Location
	Version   string
	Now       func() time.Time
	// Lookup finds a utility's published tariff (OpenEI); nil means the lookup mode is not available.
	Lookup openei.Lookup
	// Carbon reads the grid's carbon intensity; nil means the real sources.
	Carbon *carbon.Client
	// DatabasePath is the history database file, reported (read-only) by the status so the dashboard can say where the data lives and
	// how big it is. Empty reports nothing.
	DatabasePath string
}

type problem struct {
	Code             string `json:"code"`
	Message          string `json:"message"`
	ServerAPIVersion int    `json:"server_api_version,omitempty"`
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Handler returns the API's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/status", s.status)
	mux.HandleFunc("GET /api/v1/devices", s.devices)
	mux.HandleFunc("GET /api/v1/meters", s.meters)
	mux.HandleFunc("PATCH /api/v1/meters/{meterId}", s.updateMeter)
	mux.HandleFunc("GET /api/v1/meters/{meterId}/runs", s.meterRuns)
	mux.HandleFunc("GET /api/v1/meters/{meterId}/baseload", s.meterBaseLoad)
	mux.HandleFunc("GET /api/v1/baseload", s.houseBaseLoad)
	mux.HandleFunc("GET /api/v1/live", s.live)
	mux.HandleFunc("GET /api/v1/series", s.series)
	mux.HandleFunc("GET /api/v1/summary", s.summary)
	mux.HandleFunc("GET /api/v1/history", s.houseHistory)
	mux.HandleFunc("GET /api/v1/estimate", s.houseEstimate)
	mux.HandleFunc("GET /api/v1/export.csv", s.houseExport)
	mux.HandleFunc("POST /api/v1/devices", s.addDevice)
	mux.HandleFunc("PATCH /api/v1/devices/{deviceId}", s.updateDevice)
	mux.HandleFunc("DELETE /api/v1/devices/{deviceId}", s.removeDevice)
	mux.HandleFunc("GET /api/v1/rates", s.getRates)
	mux.HandleFunc("GET /api/v1/rates/lookup", s.lookupRates)
	mux.HandleFunc("GET /api/v1/rates/tariff", s.getTariff)
	mux.HandleFunc("GET /api/v1/openei", s.getOpenEi)
	mux.HandleFunc("PUT /api/v1/openei", s.putOpenEi)
	mux.HandleFunc("PUT /api/v1/rates", s.putRates)
	mux.HandleFunc("GET /api/v1/groups", s.listGroups)
	mux.HandleFunc("POST /api/v1/groups", s.addGroup)
	mux.HandleFunc("PATCH /api/v1/groups/{groupId}", s.renameGroup)
	mux.HandleFunc("DELETE /api/v1/groups/{groupId}", s.removeGroup)
	mux.HandleFunc("GET /api/v1/groups/{groupId}/detail", s.groupDetail)
	mux.HandleFunc("GET /api/v1/computers", s.listComputers)
	mux.HandleFunc("POST /api/v1/computers", s.addComputer)
	mux.HandleFunc("PATCH /api/v1/computers/{computerId}", s.updateComputer)
	mux.HandleFunc("DELETE /api/v1/computers/{computerId}", s.removeComputer)
	mux.HandleFunc("GET /api/v1/computers/{computerId}/detail", s.computerDetail)
	mux.HandleFunc("GET /api/v1/computers/{computerId}/live", s.computerLive)
	mux.HandleFunc("GET /api/v1/computers/{computerId}/history", s.computerHistory)
	mux.HandleFunc("GET /api/v1/computers/{computerId}/estimate", s.computerEstimate)
	mux.HandleFunc("GET /api/v1/computers/{computerId}/export.csv", s.computerExport)
	mux.HandleFunc("GET /api/v1/computers/{computerId}/series", s.computerSeries)
	mux.HandleFunc("POST /api/v1/ingest/pc", s.ingestPc)
	mux.HandleFunc("GET /api/v1/carbon", s.getCarbon)
	mux.HandleFunc("PUT /api/v1/carbon", s.putCarbon)
	mux.HandleFunc("DELETE /api/v1/carbon", s.deleteCarbon)
	return withVersion(mux)
}

func withVersion(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-KVG-Api-Version", "1")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Code: code, Message: msg})
}

func serverError(w http.ResponseWriter, err error) {
	writeProblem(w, http.StatusInternalServerError, "internal", err.Error())
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeProblem(w, http.StatusNotImplemented, "not_implemented", "this part of the API is not built yet")
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	st := map[string]any{
		"api_version":    Version,
		"server_version": s.Version,
		"time":           s.now().UTC(),
		"notes":          s.Collector.Notes(),
	}
	// "Local" is not an IANA zone name, so it is only reported when the server was given a real one (-tz).
	if name := s.Loc.String(); name != "Local" {
		st["time_zone"] = name
	}
	if s.DatabasePath != "" {
		st["database_path"] = s.DatabasePath
		if info, err := os.Stat(s.DatabasePath); err == nil {
			st["database_bytes"] = info.Size()
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) devices(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.Collector.Devices()})
}

func (s *Server) addDevice(w http.ResponseWriter, r *http.Request) {
	if s.Devices == nil {
		notImplemented(w, r)
		return
	}
	var nd devices.NewDevice
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&nd); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_device", "send kind, host and optionally name and scope")
		return
	}
	d, err := s.Devices.Add(r.Context(), nd)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, d)
	case errors.Is(err, devices.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "bad_device", err.Error())
	case errors.Is(err, devices.ErrUnsupported):
		writeProblem(w, http.StatusNotImplemented, "not_implemented", err.Error())
	case errors.Is(err, devices.ErrExists):
		writeProblem(w, http.StatusConflict, "device_exists", err.Error())
	case errors.Is(err, devices.ErrUnreachable):
		writeProblem(w, http.StatusBadGateway, "device_unreachable", err.Error())
	default:
		serverError(w, err)
	}
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) {
	if s.Devices == nil {
		notImplemented(w, r)
		return
	}
	var u devices.UpdateDevice
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&u); err != nil || (u.Name == nil && u.Host == nil) {
		writeProblem(w, http.StatusBadRequest, "bad_device", "send a name and/or a host")
		return
	}
	d, err := s.Devices.Update(r.Context(), r.PathValue("deviceId"), u)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, d)
	case errors.Is(err, devices.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "unknown_device", "no such device")
	case errors.Is(err, devices.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "bad_device", err.Error())
	case errors.Is(err, devices.ErrExists):
		writeProblem(w, http.StatusConflict, "device_exists", err.Error())
	case errors.Is(err, devices.ErrUnreachable):
		writeProblem(w, http.StatusBadGateway, "device_unreachable", err.Error())
	default:
		serverError(w, err)
	}
}

func (s *Server) removeDevice(w http.ResponseWriter, r *http.Request) {
	if s.Devices == nil {
		notImplemented(w, r)
		return
	}
	switch err := s.Devices.Remove(r.Context(), r.PathValue("deviceId")); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, devices.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "unknown_device", "no such device")
	default:
		serverError(w, err)
	}
}

func (s *Server) meters(w http.ResponseWriter, r *http.Request) {
	chs, err := s.Store.Meters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if chs == nil {
		chs = []model.Meter{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"meters": chs})
}

func (s *Server) updateMeter(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label      *string         `json:"label"`
		Enabled    *bool           `json:"enabled"`
		ComputerID json.RawMessage `json:"computer_id"` // a computer's id, or null to let the plug go; absent leaves it alone
		GroupID    json.RawMessage `json:"group_id"`    // an appliance group's id, or null to take the plug out; absent leaves it alone
		// How a Refoss monitor's clamp is wired: its sign, the other leg of its breaker, and whether it measures a circuit or the mains.
		Invert   *bool           `json:"invert"`
		PairWith json.RawMessage `json:"pair_with"` // another input's meter id, or null to unpair; absent leaves it alone
		Type     *string         `json:"type"`      // what a Refoss meter is: house, sub_panel or circuit
		// The circuit this one is part of (a sub-panel's feed), or null to take it out; absent leaves it alone.
		ParentID json.RawMessage `json:"parent_id"`
		// A favorite is offered on the live page as a one-click choice.
		Favorite *bool `json:"favorite"`
		// The size of the breaker the meter is on, in amps, or null to forget it; absent leaves it alone.
		BreakerAmps json.RawMessage `json:"breaker_amps"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || (body.Label == nil && body.Enabled == nil && body.ComputerID == nil && body.GroupID == nil &&
		body.Invert == nil && body.PairWith == nil && body.Type == nil && body.ParentID == nil && body.Favorite == nil && body.BreakerAmps == nil) {
		writeProblem(w, http.StatusBadRequest, "bad_request", "send a label, enabled, computer_id, group_id, invert, pair_with, type, parent_id, favorite and/or breaker_amps")
		return
	}
	if body.Label != nil {
		l := strings.TrimSpace(*body.Label)
		if l == "" || len([]rune(l)) > 60 {
			writeProblem(w, http.StatusBadRequest, "bad_label", "a label is 1 to 60 characters")
			return
		}
		body.Label = &l
	}
	var computerID *string
	if body.ComputerID != nil {
		if err := json.Unmarshal(body.ComputerID, &computerID); err != nil {
			writeProblem(w, http.StatusBadRequest, "bad_request", "computer_id is a computer's id or null")
			return
		}
	}

	var groupID *string
	if body.GroupID != nil {
		if err := json.Unmarshal(body.GroupID, &groupID); err != nil {
			writeProblem(w, http.StatusBadRequest, "bad_request", "group_id is a group's id or null")
			return
		}
	}

	var pairWith *string
	if body.PairWith != nil {
		if err := json.Unmarshal(body.PairWith, &pairWith); err != nil {
			writeProblem(w, http.StatusBadRequest, "bad_request", "pair_with is another input's meter id or null")
			return
		}
	}
	var parentID *string
	if body.ParentID != nil {
		if err := json.Unmarshal(body.ParentID, &parentID); err != nil {
			writeProblem(w, http.StatusBadRequest, "bad_request", "parent_id is a circuit's meter id or null")
			return
		}
	}
	var breaker *float64
	if body.BreakerAmps != nil {
		if err := json.Unmarshal(body.BreakerAmps, &breaker); err != nil {
			writeProblem(w, http.StatusBadRequest, "bad_request", "breaker_amps is a number of amps or null")
			return
		}
	}
	var mtype *model.MeterType
	if body.Type != nil {
		t := model.MeterType(*body.Type)
		mtype = &t
	}

	id := r.PathValue("meterId")
	ch, err := s.Store.UpdateMeter(r.Context(), id, body.Label, body.Enabled)
	if err == nil && (body.Invert != nil || body.PairWith != nil || mtype != nil) {
		ch, err = s.Store.SetMeterWiring(r.Context(), id, store.WiringChange{Invert: body.Invert, SetPair: body.PairWith != nil, PairWith: pairWith, Type: mtype})
	}
	if err == nil && body.ParentID != nil {
		ch, err = s.Store.SetMeterParent(r.Context(), id, parentID)
	}
	if err == nil && body.Favorite != nil {
		ch, err = s.Store.SetMeterFavorite(r.Context(), id, *body.Favorite)
	}
	if err == nil && body.BreakerAmps != nil {
		ch, err = s.Store.SetMeterBreaker(r.Context(), id, breaker)
	}
	if err == nil && body.ComputerID != nil {
		ch, err = s.Store.AssignMeter(r.Context(), id, computerID)
	}
	if err == nil && body.GroupID != nil {
		ch, err = s.Store.AssignMeterToGroup(r.Context(), id, groupID)
	}
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, ch)
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "unknown_meter", "no such meter")
	case errors.Is(err, store.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "bad_computer", err.Error())
	case errors.Is(err, store.ErrConflict):
		writeProblem(w, http.StatusConflict, "computer_conflict", err.Error())
	default:
		serverError(w, err)
	}
}

// enabledMeters returns the meters the user has not switched off, keyed by id.
func (s *Server) enabledMeters(ctx context.Context) ([]model.Meter, map[string]model.Meter, error) {
	chs, err := s.Store.Meters(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]model.Meter{}
	var on []model.Meter
	for _, c := range chs {
		byID[c.ID] = c
		if c.Enabled {
			on = append(on, c)
		}
	}
	return on, byID, nil
}

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	readings := []model.Reading{}
	var house float64
	haveMains := false
	for _, rd := range s.Collector.LiveReadings() {
		ch, ok := byID[rd.MeterID]
		if !ok || !ch.Enabled {
			continue
		}
		readings = append(readings, rd)
		if ch.Scope == model.ScopeMains {
			house += rd.Watts
			haveMains = true
		}
	}
	live := model.Live{Time: s.now().UTC(), Readings: readings, Notes: s.Collector.Notes()}
	if haveMains {
		live.HouseWatts = &house
	}
	writeJSON(w, http.StatusOK, live)
}

func parsePeriod(r *http.Request) (from, to time.Time, err error) {
	if from, err = time.Parse(time.RFC3339, r.URL.Query().Get("from")); err != nil {
		return from, to, errors.New("from must be an RFC 3339 time")
	}
	if to, err = time.Parse(time.RFC3339, r.URL.Query().Get("to")); err != nil {
		return from, to, errors.New("to must be an RFC 3339 time")
	}
	if !to.After(from) {
		return from, to, errors.New("to must be after from")
	}
	return from, to, nil
}

// defaultBucket is the coarsest bucket that keeps the period at about 400 points or fewer.
func defaultBucket(span time.Duration) model.Bucket {
	switch {
	case span <= 400*time.Minute:
		return model.BucketMinute
	case span <= 400*time.Hour:
		return model.BucketHour
	default:
		return model.BucketDay
	}
}

func (s *Server) series(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	id := r.URL.Query().Get("meter_id")
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if _, ok := byID[id]; !ok {
		writeProblem(w, http.StatusBadRequest, "unknown_meter", "no such meter")
		return
	}
	bucket := model.Bucket(r.URL.Query().Get("bucket"))
	switch bucket {
	case "":
		bucket = defaultBucket(to.Sub(from))
	case model.BucketMinute, model.BucketHour, model.BucketDay:
	default:
		writeProblem(w, http.StatusBadRequest, "bad_bucket", "bucket is minute, hour or day")
		return
	}
	points, err := s.Store.Series(r.Context(), id, from, to, bucket, s.Loc)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model.Series{MeterID: id, Bucket: bucket, Points: points})
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)

	sum := model.Summary{From: from.UTC(), To: to.UTC(), Meters: []model.MeterSummary{}}
	// A circuit inside a sub-panel is part of that panel's feed, which is a circuit itself: only the top-level circuits are taken off the house.
	shown := map[string]bool{}
	for _, ch := range on {
		if ch.Scope == model.ScopeCircuit {
			shown[ch.ID] = true
		}
	}
	var house, circuits float64
	var houseCost rates.Money
	haveMains, housePriced := false, true
	var housePeak, houseCarbon, houseCarbonEst float64
	havePeak, haveCarbon := false, false
	for _, ch := range on {
		cs, priced, err := s.meterSummary(r.Context(), ch, from, to, schedule)
		if err != nil {
			serverError(w, err)
			return
		}
		sum.CostEstimated = sum.CostEstimated || (cs.Cost != nil && priced.Estimated)
		sum.Meters = append(sum.Meters, cs)

		// Scopes are never summed across each other: the house is the mains alone, circuits are summed on their own.
		switch ch.Scope {
		case model.ScopeMains:
			house += cs.KWh
			haveMains = true
			if cs.PeakWatts != nil {
				housePeak += *cs.PeakWatts
				havePeak = true
			}
			if cs.CarbonG != nil {
				houseCarbon += *cs.CarbonG
				haveCarbon = true
				if cs.CarbonEstimatedKWh != nil {
					houseCarbonEst += *cs.CarbonEstimatedKWh
				}
			}
			if priced.Unpriced {
				housePriced = false
			} else {
				houseCost = houseCost.Add(priced.Cost)
			}
		case model.ScopeCircuit:
			if ch.ParentID == nil || !shown[*ch.ParentID] {
				circuits += cs.KWh
			}
		}
	}
	if haveMains {
		sum.HouseKWh = &house
		unmetered := house - circuits
		if unmetered < 0 {
			unmetered = 0
		}
		sum.UnmeteredKWh = &unmetered
		if housePriced {
			c := houseCost.String()
			sum.HouseCost = &c
		}
		if havePeak {
			sum.HousePeakWatts = &housePeak
		}
		if haveCarbon {
			sum.HouseCarbonG, sum.HouseCarbonEstimatedKWh = &houseCarbon, &houseCarbonEst
		}
	}
	writeJSON(w, http.StatusOK, sum)
}

// meterSummary is one meter's energy, peak, covered time and cost over [from, to). Each stretch of the period is priced
// at its own day's rate (a period can span two bills); the cost is nil when no price is known.
func (s *Server) meterSummary(ctx context.Context, ch model.Meter, from, to time.Time, schedule *rates.Schedule) (model.MeterSummary, rates.RatedCost, error) {
	t, err := s.Store.Totals(ctx, ch.ID, from, to)
	if err != nil {
		return model.MeterSummary{}, rates.RatedCost{}, err
	}
	cs := model.MeterSummary{MeterID: ch.ID, ComputerID: ch.ComputerID, GroupID: ch.GroupID, Scope: ch.Scope, KWh: t.KWh, MonitoredHours: t.MonitoredHours}
	if t.MonitoredHours > 0 {
		peak := t.PeakWatts
		cs.PeakWatts = &peak
	}
	cs.PeakAmps = t.PeakAmps
	var readErr error
	priced := schedule.Cost(func(f, end time.Time) float64 {
		tot, err := s.Store.Totals(ctx, ch.ID, f, end)
		if err != nil && readErr == nil {
			readErr = err
		}
		return tot.KWh
	}, from, to)
	if readErr != nil {
		return model.MeterSummary{}, rates.RatedCost{}, readErr
	}
	if !schedule.HasRate(from, to) {
		priced.Unpriced = true // no price known: a meter that used nothing has no cost either, not "$0.00"
	}
	if !priced.Unpriced {
		c := priced.Cost.String()
		cs.Cost = &c
	}
	car, err := s.carbonOf(ctx, []string{ch.ID}, from, to)
	if err != nil {
		return model.MeterSummary{}, rates.RatedCost{}, err
	}
	if car != nil {
		g, est := car.Kg*1000, car.EstimatedKWh
		cs.CarbonG, cs.CarbonEstimatedKWh = &g, &est
	}
	return cs, priced, nil
}

const maxIngestBody = 16 << 20

// ingestPc stores what the PC collector pushed. Sending the same minutes again replaces them, so the collector can
// resend its backlog after a failure. Unknown fields are ignored (a newer collector may send more than this server reads).
func (s *Server) ingestPc(w http.ResponseWriter, r *http.Request) {
	var in pcrelay.Ingest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxIngestBody)).Decode(&in); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_ingest", "the body is not a valid PC push: "+err.Error())
		return
	}
	notes, err := in.Validate(s.now())
	switch {
	case errors.Is(err, pcrelay.ErrVersion):
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUpgradeRequired)
		_ = json.NewEncoder(w).Encode(problem{Code: "api_version_mismatch", Message: err.Error(), ServerAPIVersion: Version})
		return
	case err != nil:
		writeProblem(w, http.StatusBadRequest, "bad_ingest", err.Error())
		return
	}
	accepted, err := s.Store.IngestPC(r.Context(), in, s.now())
	if err != nil {
		serverError(w, err)
		return
	}
	if notes == nil {
		notes = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_version": Version, "accepted": accepted, "notes": notes})
}

func groupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "unknown_group", "no such group")
	case errors.Is(err, store.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "bad_group", err.Error())
	case errors.Is(err, store.ErrConflict):
		writeProblem(w, http.StatusConflict, "group_conflict", err.Error())
	default:
		serverError(w, err)
	}
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	all, err := s.Store.Groups(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": all})
}

func decodeName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Name string `json:"name"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_group", "send a name")
		return "", false
	}
	return body.Name, true
}

func (s *Server) addGroup(w http.ResponseWriter, r *http.Request) {
	name, ok := decodeName(w, r)
	if !ok {
		return
	}
	g, err := s.Store.CreateGroup(r.Context(), name)
	if err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) renameGroup(w http.ResponseWriter, r *http.Request) {
	name, ok := decodeName(w, r)
	if !ok {
		return
	}
	g, err := s.Store.RenameGroup(r.Context(), r.PathValue("groupId"), name)
	if err != nil {
		groupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) removeGroup(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteGroup(r.Context(), r.PathValue("groupId")); err != nil {
		groupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// groupDetail is a group's energy and cost for a period: each plug on its own and their total. The plugs are all ordinary plugs
// (one scope), so the total is a sound sum; a plug the user has hidden is neither shown nor counted.
func (s *Server) groupDetail(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	g, err := s.Store.Group(r.Context(), r.PathValue("groupId"))
	if err != nil {
		groupError(w, err)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}

	detail := model.GroupDetail{Group: g, From: from.UTC(), To: to.UTC(), Members: []model.MeterSummary{}, NotAdded: []string{}}
	var cost rates.Money
	priced := true
	inGroup := map[string]bool{}
	for _, id := range g.MeterIDs {
		if ch, ok := byID[id]; ok && ch.Enabled {
			inGroup[id] = true
		}
	}
	// Inside another member (its parent, or the parent's parent...): already in that member's reading.
	held := func(ch model.Meter) bool {
		for seen := 0; ch.ParentID != nil && seen < 16; seen++ {
			if inGroup[*ch.ParentID] {
				return true
			}
			next, ok := byID[*ch.ParentID]
			if !ok {
				return false
			}
			ch = next
		}
		return false
	}
	for _, id := range g.MeterIDs {
		ch, ok := byID[id]
		if !ok || !ch.Enabled {
			continue
		}
		cs, rc, err := s.meterSummary(r.Context(), ch, from, to, schedule)
		if err != nil {
			serverError(w, err)
			return
		}
		detail.Members = append(detail.Members, cs)
		if held(ch) {
			detail.NotAdded = append(detail.NotAdded, id)
			continue
		}
		detail.TotalKWh += cs.KWh
		if rc.Unpriced {
			priced = false
		} else {
			cost = cost.Add(rc.Cost)
			detail.CostEstimated = detail.CostEstimated || rc.Estimated
		}
	}
	if priced && len(detail.Members) > 0 {
		c := cost.String()
		detail.TotalCost = &c
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) listComputers(w http.ResponseWriter, r *http.Request) {
	all, err := s.Store.Computers(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"computers": all})
}

func computerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "unknown_computer", "no such computer")
	case errors.Is(err, store.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "bad_computer", err.Error())
	case errors.Is(err, store.ErrConflict):
		writeProblem(w, http.StatusConflict, "computer_conflict", err.Error())
	default:
		serverError(w, err)
	}
}

func (s *Server) addComputer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string  `json:"name"`
		SourceID *string `json:"source_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_computer", "send a name and optionally source_id")
		return
	}
	c, err := s.Store.CreateComputer(r.Context(), body.Name, body.SourceID)
	if err != nil {
		computerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) updateComputer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     *string         `json:"name"`
		SourceID json.RawMessage `json:"source_id"` // a collector's id, or null to stop using one; absent leaves it alone
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || (body.Name == nil && body.SourceID == nil) {
		writeProblem(w, http.StatusBadRequest, "bad_computer", "send a name and/or source_id")
		return
	}
	var source *string
	if body.SourceID != nil {
		if err := json.Unmarshal(body.SourceID, &source); err != nil {
			writeProblem(w, http.StatusBadRequest, "bad_computer", "source_id is a collector's id or null")
			return
		}
	}
	c, err := s.Store.UpdateComputer(r.Context(), r.PathValue("computerId"), body.Name, body.SourceID != nil, source)
	if err != nil {
		computerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) removeComputer(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteComputer(r.Context(), r.PathValue("computerId")); err != nil {
		computerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// computerDetail is everything about one computer for a period: the whole PC at its wall plug, its peripherals, and what its
// collector reported about its insides. "Setup" adds the wall figure and the peripherals for the whole desk; it is its own
// labelled figure and is never part of the PC's, the circuits' or the house's.
func (s *Server) computerDetail(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	comp, err := s.Store.Computer(r.Context(), r.PathValue("computerId"))
	if err != nil {
		computerError(w, err)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}

	detail := model.ComputerDetail{Computer: comp, From: from.UTC(), To: to.UTC(), Peripherals: []model.MeterSummary{}}
	var setup float64
	var setupCost rates.Money
	setupPriced, haveAny := true, false
	add := func(id string) (*model.MeterSummary, error) {
		ch, ok := byID[id]
		if !ok || !ch.Enabled {
			return nil, nil // a plug the user has hidden is neither shown nor counted
		}
		cs, priced, err := s.meterSummary(r.Context(), ch, from, to, schedule)
		if err != nil {
			return nil, err
		}
		setup += cs.KWh
		haveAny = true
		if priced.Unpriced {
			setupPriced = false
		} else {
			setupCost = setupCost.Add(priced.Cost)
		}
		detail.CostEstimated = detail.CostEstimated || (cs.Cost != nil && priced.Estimated)
		return &cs, nil
	}
	if comp.WallMeterID != nil {
		if detail.Wall, err = add(*comp.WallMeterID); err != nil {
			serverError(w, err)
			return
		}
	}
	for _, id := range comp.PeripheralMeterIDs {
		cs, err := add(id)
		if err != nil {
			serverError(w, err)
			return
		}
		if cs != nil {
			detail.Peripherals = append(detail.Peripherals, *cs)
		}
	}
	if haveAny {
		detail.SetupKWh = &setup
		if setupPriced {
			c := setupCost.String()
			detail.SetupCost = &c
		}
	}
	if comp.SourceID != nil {
		if detail.PC, err = s.Store.PcSourceSummary(r.Context(), *comp.SourceID, from, to); err != nil {
			serverError(w, err)
			return
		}
		if detail.PC != nil {
			priceProgramsAndRuns(detail.PC, schedule, from, to, s.Loc)
			if err := s.pcCarbon(r.Context(), detail.PC, from, to); err != nil {
				serverError(w, err)
				return
			}
		}
		if detail.PC != nil && detail.PC.Live != nil {
			// How old the PC's look is, by this server's clock: the PC's own clock is not trusted for it.
			detail.PC.Live.AgeSeconds = max(0, s.now().Sub(detail.PC.Live.ReceivedAt).Seconds())
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

// pcCarbon puts the carbon of what the PC's sensors measured on the PC's summary: each hour's energy at that hour's grid intensity.
func (s *Server) pcCarbon(ctx context.Context, pc *model.PcSource, from, to time.Time) error {
	ever, ok, err := s.Store.AverageCarbonEver(ctx)
	if err != nil || !ok {
		return err
	}
	known, err := s.Store.QueryCarbon(ctx, from, to)
	if err != nil {
		return err
	}
	byHour, err := s.Store.PcHourlyKWh(ctx, pc.SourceID, from, to)
	if err != nil {
		return err
	}
	r := carbonResult(known, ever, byHour)
	g, est := r.Kg*1000, r.EstimatedKWh
	pc.CarbonG, pc.CarbonEstimatedKWh = &g, &est
	return nil
}

// priceProgramsAndRuns puts a cost on each program (its energy at the period's average price, one number for many small amounts) and
// on each run (at the price on the day it ended). Both are estimates because the energy is. Nothing is priced without a known price.
func priceProgramsAndRuns(pc *model.PcSource, schedule *rates.Schedule, from, to time.Time, loc *time.Location) {
	avg := schedule.AverageRate(from, to)
	for i := range pc.Programs {
		if avg != nil {
			c := rates.MoneyFromFloat(pc.Programs[i].KWh).Mul(*avg).String()
			pc.Programs[i].Cost = &c
		}
	}
	for i := range pc.Runs {
		if rate := schedule.RateAt(rates.DateOf(pc.Runs[i].End, loc)); rate != nil {
			c := rates.MoneyFromFloat(pc.Runs[i].CPUKwh + pc.Runs[i].GPUKwh).Mul(*rate).String()
			pc.Runs[i].Cost = &c
		}
	}
}

// historyPeriods are the rolling periods of a computer's energy history, ending now. "All" starts long before any data.
var historyPeriods = []struct {
	key, label string
	span       time.Duration
}{
	{"1h", "Last hour", time.Hour},
	{"24h", "Last 24 hours", 24 * time.Hour},
	{"7d", "Last 7 days", 7 * 24 * time.Hour},
	{"30d", "Last 30 days", 30 * 24 * time.Hour},
	{"1y", "Last year", 365 * 24 * time.Hour},
	{"all", "All time", 0},
}

var allTimeFrom = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// computerHistory is one row per rolling period: the whole PC at its wall plug (else what its sensors measured), what that cost, and how
// much of the period nothing was recorded, by reason. The cost is exact with a wall plug and an estimate (the period's average price)
// from sensors alone, which is said so.
func (s *Server) computerHistory(w http.ResponseWriter, r *http.Request) {
	comp, err := s.Store.Computer(r.Context(), r.PathValue("computerId"))
	if err != nil {
		computerError(w, err)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}

	to := s.now().Truncate(time.Minute).Add(time.Minute)
	out := model.ComputerHistory{Periods: []model.PcHistoryPeriod{}}
	for _, p := range historyPeriods {
		from := allTimeFrom
		if p.span > 0 {
			from = to.Add(-p.span)
		}
		row := model.PcHistoryPeriod{Key: p.key, Label: p.label, From: from.UTC(), To: to.UTC(), Basis: "sensors"}

		if comp.SourceID != nil {
			if row.SensorsKWh, err = s.Store.PcSensorsKwh(r.Context(), *comp.SourceID, from, to); err != nil {
				serverError(w, err)
				return
			}
			gaps, err := s.Store.PcGaps(r.Context(), *comp.SourceID, from, to)
			if err != nil {
				serverError(w, err)
				return
			}
			for _, g := range gaps {
				secs := g.To.Sub(g.From).Seconds()
				switch g.Reason {
				case "asleep":
					row.AsleepSeconds += secs
				case "off":
					row.OffSeconds += secs
				default:
					row.LostPowerSeconds += secs
				}
				if g.PlugKwh != nil {
					row.PlugGapKWh += *g.PlugKwh
				}
			}
		}

		if ch, ok := byID[derefString(comp.WallMeterID)]; ok && ch.Enabled {
			cs, priced, err := s.meterSummary(r.Context(), ch, from, to, schedule)
			if err != nil {
				serverError(w, err)
				return
			}
			row.KWh, row.Basis, row.Cost, row.CostEstimated = cs.KWh, "wall", cs.Cost, cs.Cost != nil && priced.Estimated
		} else {
			row.KWh = row.SensorsKWh
			if avg := schedule.AverageRate(from, to); avg != nil && comp.SourceID != nil {
				c := rates.MoneyFromFloat(row.KWh).Mul(*avg).String()
				row.Cost, row.CostEstimated = &c, true
			}
		}
		out.Periods = append(out.Periods, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// minimumForAverage: less recorded time than this gives an average too noisy to project from.
const minimumForAverage = time.Hour

// computerEstimate gives the cost estimator today's price and what the computer really used per day over a window ("24h", "7d", "30d",
// "all"; default 7d). The whole PC at its wall plug is used if it recorded anything in the window, otherwise the sensors; with
// include_peripherals the plugs next to it are added. It averages over the time actually recorded, and says nothing (usage null) when
// less than an hour has been.
func (s *Server) computerEstimate(w http.ResponseWriter, r *http.Request) {
	comp, err := s.Store.Computer(r.Context(), r.PathValue("computerId"))
	if err != nil {
		computerError(w, err)
		return
	}
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
	includePC := r.URL.Query().Get("include_pc") != "false"
	wanted := map[string]bool{}
	for _, id := range strings.Split(r.URL.Query().Get("peripherals"), ",") {
		if id != "" {
			wanted[id] = true
		}
	}

	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	// Only the plugs that belong to this computer, are shown, and were asked for are counted.
	var plugs []model.Meter
	for _, id := range comp.PeripheralMeterIDs {
		if ch, ok := byID[id]; ok && ch.Enabled && wanted[id] {
			plugs = append(plugs, ch)
		}
	}

	var kwh float64
	var whole bool
	var first time.Time
	var have bool
	earliest := func(t time.Time, ok bool) {
		if ok && (!have || t.Before(first)) {
			first, have = t, true
		}
	}
	if includePC {
		if ch, ok := byID[derefString(comp.WallMeterID)]; ok && ch.Enabled {
			t, err := s.Store.Totals(r.Context(), ch.ID, from, now)
			if err != nil {
				serverError(w, err)
				return
			}
			if t.KWh > 0 {
				ft, ok, err := s.Store.FirstMinute(r.Context(), ch.ID)
				if err != nil {
					serverError(w, err)
					return
				}
				earliest(ft, ok)
				whole = true
			}
		}
		if !whole && comp.SourceID != nil {
			ft, ok, err := s.Store.PcFirstSensorMinute(r.Context(), *comp.SourceID)
			if err != nil {
				serverError(w, err)
				return
			}
			earliest(ft, ok)
		}
	}
	for _, ch := range plugs {
		ft, ok, err := s.Store.FirstMinute(r.Context(), ch.ID)
		if err != nil {
			serverError(w, err)
			return
		}
		if !includePC {
			earliest(ft, ok)
		}
	}
	if !have {
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

	if includePC {
		if whole {
			t, err := s.Store.Totals(r.Context(), derefString(comp.WallMeterID), from, now)
			if err != nil {
				serverError(w, err)
				return
			}
			kwh = t.KWh
		} else if kwh, err = s.Store.PcSensorsKwh(r.Context(), *comp.SourceID, from, now); err != nil {
			serverError(w, err)
			return
		}
	}
	for _, ch := range plugs {
		t, err := s.Store.Totals(r.Context(), ch.ID, from, now)
		if err != nil {
			serverError(w, err)
			return
		}
		kwh += t.KWh
	}
	days := now.Sub(from).Hours() / 24
	out.Usage = &model.AverageUse{
		KWh: kwh, Days: days, KWhPerDay: kwh / days, AverageWatts: kwh / days * 1000 / 24,
		WholePC: whole, IncludesPC: includePC, From: from.UTC(), PeripheralCount: len(plugs),
	}
	writeJSON(w, http.StatusOK, out)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// computerSeries is the PC collector's sensors, wall plug and meters over time, one line each, with the gaps to shade. It is the
// detail the per-plug series cannot give: what each part of the PC was drawing. 204 when no collector reports for the computer.
func (s *Server) computerSeries(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	bucket := model.Bucket(r.URL.Query().Get("bucket"))
	switch bucket {
	case "":
		bucket = defaultBucket(to.Sub(from))
	case model.BucketMinute, model.BucketHour, model.BucketDay:
	default:
		writeProblem(w, http.StatusBadRequest, "bad_bucket", "bucket is minute, hour or day")
		return
	}
	comp, err := s.Store.Computer(r.Context(), r.PathValue("computerId"))
	if err != nil {
		computerError(w, err)
		return
	}
	if comp.SourceID == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	series, err := s.Store.PcSeries(r.Context(), *comp.SourceID, from, to, bucket, s.Loc)
	if err != nil {
		serverError(w, err)
		return
	}
	if series == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, series)
}

// computerLive is only the PC's latest look, which the dashboard asks for every few seconds: the detail answer re-reads a whole
// period, this reads one row. 204 when no collector reports for the computer or none has sent a look yet.
func (s *Server) computerLive(w http.ResponseWriter, r *http.Request) {
	comp, err := s.Store.Computer(r.Context(), r.PathValue("computerId"))
	if err != nil {
		computerError(w, err)
		return
	}
	if comp.SourceID == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	live, err := s.Store.PcLive(r.Context(), *comp.SourceID)
	if err != nil {
		serverError(w, err)
		return
	}
	if live == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	live.AgeSeconds = max(0, s.now().Sub(live.ReceivedAt).Seconds())
	writeJSON(w, http.StatusOK, live)
}

const ratesKey = "rates"

// rateConfig is the saved price settings, or the default (one rate, none set) before any are saved.
func (s *Server) rateConfig(ctx context.Context) (rates.Config, error) {
	text, ok, err := s.Store.Meta(ctx, ratesKey)
	if err != nil || !ok {
		return rates.DefaultConfig(), err
	}
	var cfg rates.Config
	if err := json.Unmarshal([]byte(text), &cfg); err != nil {
		return rates.Config{}, err
	}
	cfg = cfg.Normalized()
	if cfg.Mode == rates.ModeLookup {
		if cfg.Tariff, err = s.savedTariff(ctx); err != nil {
			return rates.Config{}, err
		}
		if cfg.Profile, err = s.savedProfile(ctx); err != nil {
			return rates.Config{}, err
		}
	}
	return cfg, nil
}

func (s *Server) getRates(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) putRates(w http.ResponseWriter, r *http.Request) {
	var cfg rates.Config
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_rates", err.Error())
		return
	}
	cfg = cfg.Normalized()
	if err := cfg.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_rates", err.Error())
		return
	}
	if cfg.Mode == rates.ModeLookup {
		// The chosen rate is read again from OpenEI by its label and kept; a failure says why and leaves the saved prices as they were.
		if code, msg, err := s.chooseTariff(r.Context(), derefString(cfg.TariffLabel)); err != nil {
			serverError(w, err)
			return
		} else if code != 0 {
			writeProblem(w, code, "bad_rates", msg)
			return
		}
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := s.Store.SetMeta(r.Context(), ratesKey, string(body)); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}
