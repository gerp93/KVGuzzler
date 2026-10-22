// Package api serves the house API described in contract/openapi.yaml.
//
// Implemented so far: status, devices (read), channels (read, rename, switch off), live, series and summary.
// The rest of the contract (adding devices, rates, carbon, the PC ingest) answers 501 until it is built.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// Version is the contract's major version this server speaks.
const Version = 1

type Server struct {
	Store     *store.Store
	Collector *collector.Collector
	Loc       *time.Location
	Version   string
	Now       func() time.Time
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
	mux.HandleFunc("GET /api/v1/channels", s.channels)
	mux.HandleFunc("PATCH /api/v1/channels/{channelId}", s.updateChannel)
	mux.HandleFunc("GET /api/v1/live", s.live)
	mux.HandleFunc("GET /api/v1/series", s.series)
	mux.HandleFunc("GET /api/v1/summary", s.summary)
	for _, route := range []string{
		"POST /api/v1/devices", "DELETE /api/v1/devices/{deviceId}",
		"GET /api/v1/rates", "PUT /api/v1/rates", "GET /api/v1/carbon", "PUT /api/v1/carbon",
		"POST /api/v1/ingest/pc",
	} {
		mux.HandleFunc(route, notImplemented)
	}
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
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) devices(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": s.Collector.Devices()})
}

func (s *Server) channels(w http.ResponseWriter, r *http.Request) {
	chs, err := s.Store.Channels(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if chs == nil {
		chs = []model.Channel{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": chs})
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label   *string `json:"label"`
		Enabled *bool   `json:"enabled"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || (body.Label == nil && body.Enabled == nil) {
		writeProblem(w, http.StatusBadRequest, "bad_request", "send a label and/or enabled")
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
	ch, err := s.Store.UpdateChannel(r.Context(), r.PathValue("channelId"), body.Label, body.Enabled)
	if errors.Is(err, store.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "unknown_channel", "no such channel")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

// enabledChannels returns the channels the user has not switched off, keyed by id.
func (s *Server) enabledChannels(ctx context.Context) ([]model.Channel, map[string]model.Channel, error) {
	chs, err := s.Store.Channels(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]model.Channel{}
	var on []model.Channel
	for _, c := range chs {
		byID[c.ID] = c
		if c.Enabled {
			on = append(on, c)
		}
	}
	return on, byID, nil
}

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	_, byID, err := s.enabledChannels(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	readings := []model.Reading{}
	var house float64
	haveMains := false
	for _, rd := range s.Collector.LiveReadings() {
		ch, ok := byID[rd.ChannelID]
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
	id := r.URL.Query().Get("channel_id")
	_, byID, err := s.enabledChannels(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if _, ok := byID[id]; !ok {
		writeProblem(w, http.StatusBadRequest, "unknown_channel", "no such channel")
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
	writeJSON(w, http.StatusOK, model.Series{ChannelID: id, Bucket: bucket, Points: points})
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	on, _, err := s.enabledChannels(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	sum := model.Summary{From: from.UTC(), To: to.UTC(), Channels: []model.ChannelSummary{}}
	var house, circuits float64
	haveMains := false
	for _, ch := range on {
		t, err := s.Store.Totals(r.Context(), ch.ID, from, to)
		if err != nil {
			serverError(w, err)
			return
		}
		cs := model.ChannelSummary{ChannelID: ch.ID, Scope: ch.Scope, KWh: t.KWh, MonitoredHours: t.MonitoredHours}
		if t.MonitoredHours > 0 {
			peak := t.PeakWatts
			cs.PeakWatts = &peak
		}
		sum.Channels = append(sum.Channels, cs)
		// Scopes are never summed across each other: the house is the mains alone, circuits are summed on their own.
		switch ch.Scope {
		case model.ScopeMains:
			house += t.KWh
			haveMains = true
		case model.ScopeCircuit:
			circuits += t.KWh
		}
	}
	if haveMains {
		sum.HouseKWh = &house
		unmetered := house - circuits
		if unmetered < 0 {
			unmetered = 0
		}
		sum.UnmeteredKWh = &unmetered
	}
	writeJSON(w, http.StatusOK, sum)
}
