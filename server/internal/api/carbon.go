package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/carbon"
	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// The saved pieces of the carbon footprint: the key and place (the key is write-only and goes only to EIA) in the private secrets file, not the
// database, and what the last fetch said in the meta table.
const (
	carbonConfigKey = "carbon_config"
	carbonStatusKey = "carbon_status"
)

func (s *Server) carbonClient() *carbon.Client {
	if s.Carbon != nil {
		return s.Carbon
	}
	return &carbon.Client{}
}

// LoadCarbonConfig is the saved setup as it will be used, or nil when none is set up or what is saved cannot be used.
func (s *Server) LoadCarbonConfig(ctx context.Context) (*carbon.Config, error) {
	text, ok, err := s.Store.Secret(ctx, carbonConfigKey)
	if err != nil || !ok || text == "" {
		return nil, err
	}
	var c carbon.Config
	if json.Unmarshal([]byte(text), &c) != nil {
		return nil, nil
	}
	valid, ok := c.Valid()
	if !ok {
		return nil, nil
	}
	return &valid, nil
}

func (s *Server) carbonStatus(ctx context.Context) (*carbon.Status, error) {
	text, ok, err := s.Store.Meta(ctx, carbonStatusKey)
	if err != nil || !ok {
		return nil, err
	}
	var st carbon.Status
	if json.Unmarshal([]byte(text), &st) != nil {
		return nil, nil
	}
	return &st, nil
}

// carbonSink lets the carbon feed save what it reads in this server's database.
type carbonSink struct{ s *Server }

func (k carbonSink) AddCarbon(ctx context.Context, at time.Time, g float64, estimated bool) error {
	return k.s.Store.AddCarbon(ctx, at, g, estimated)
}

func (k carbonSink) SaveStatus(ctx context.Context, st carbon.Status) error {
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return k.s.Store.SetMeta(ctx, carbonStatusKey, string(body))
}

// CarbonFeed is the feed that keeps the saved grid intensity up to date; the server's main runs it.
func (s *Server) CarbonFeed() *carbon.Feed {
	return &carbon.Feed{Client: s.carbonClient(), Load: s.LoadCarbonConfig, Sink: carbonSink{s}, Now: s.now}
}

func (s *Server) carbonView(ctx context.Context) (model.CarbonConfig, error) {
	out := model.CarbonConfig{}
	cfg, err := s.LoadCarbonConfig(ctx)
	if err != nil {
		return out, err
	}
	if cfg != nil {
		out.Configured, out.Place = true, cfg.Place
	}
	st, err := s.carbonStatus(ctx)
	if err != nil || st == nil {
		return out, err
	}
	checked := st.CheckedAt
	out.LastFetch = &checked
	if st.OK {
		msg := "Reading saved."
		out.LastOutcome = &msg
		out.LastGrams = st.GramsPerKwh
		out.LastHour = st.At
	} else {
		out.LastOutcome = &st.Error
	}
	return out, nil
}

func (s *Server) getCarbon(w http.ResponseWriter, r *http.Request) {
	v, err := s.carbonView(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// putCarbon saves the place and, optionally, a new key. It only keeps a setup that got a reading, so a wrong key or place is said so at once
// instead of failing quietly later. The key is write-only: omit it to keep the saved one.
func (s *Server) putCarbon(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Place string  `json:"place"`
		Key   *string `json:"key"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_carbon", "send place and (the first time) key")
		return
	}
	key := ""
	if body.Key != nil {
		key = strings.TrimSpace(*body.Key)
	}
	if key == "" {
		// A key left out keeps the saved one.
		if have, err := s.LoadCarbonConfig(r.Context()); err != nil {
			serverError(w, err)
			return
		} else if have != nil {
			key = have.APIKey
		}
	}
	if msg := carbon.Problem(key, body.Place); msg != "" {
		writeProblem(w, http.StatusBadRequest, "bad_carbon", msg)
		return
	}
	cfg, _ := carbon.Config{APIKey: key, Place: body.Place}.Valid()

	readings, err := s.carbonClient().Read(r.Context(), cfg, s.now())
	if err != nil {
		var ce *carbon.Error
		if errors.As(err, &ce) {
			writeProblem(w, http.StatusBadRequest, "bad_carbon", "Not saved. "+ce.Message)
			return
		}
		serverError(w, err)
		return
	}
	text, err := json.Marshal(cfg)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := s.Store.SetSecret(r.Context(), carbonConfigKey, string(text)); err != nil {
		serverError(w, err)
		return
	}
	sink := carbonSink{s}
	for _, rd := range readings {
		if err := sink.AddCarbon(r.Context(), rd.At, rd.GramsPerKwh, rd.Estimated); err != nil {
			serverError(w, err)
			return
		}
	}
	latest := readings[len(readings)-1]
	g, at := latest.GramsPerKwh, latest.At
	if err := sink.SaveStatus(r.Context(), carbon.Status{OK: true, CheckedAt: s.now().UTC(), GramsPerKwh: &g, At: &at}); err != nil {
		serverError(w, err)
		return
	}
	v, err := s.carbonView(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// deleteCarbon removes the key and the place. The hours already saved stay, so the footprint of past energy is still shown.
func (s *Server) deleteCarbon(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.SetSecret(r.Context(), carbonConfigKey, ""); err != nil {
		serverError(w, err)
		return
	}
	_ = carbonSink{s}.SaveStatus(r.Context(), carbon.Status{CheckedAt: s.now().UTC(), Error: "Not set up."})
	w.WriteHeader(http.StatusNoContent)
}

// carbonOf is the carbon of some meters' energy over [from, to): each hour's kWh at that hour's intensity (see carbon.Value). Nil when no
// intensity has ever been saved. All the meters must be of one scope, as with any sum here.
func (s *Server) carbonOf(ctx context.Context, meterIDs []string, from, to time.Time) (*carbon.Result, error) {
	ever, ok, err := s.Store.AverageCarbonEver(ctx)
	if err != nil || !ok {
		return nil, err
	}
	known, err := s.Store.QueryCarbon(ctx, from, to)
	if err != nil {
		return nil, err
	}
	byHour, err := s.Store.HourlyKWh(ctx, meterIDs, from, to)
	if err != nil {
		return nil, err
	}
	return carbonResult(known, ever, byHour), nil
}

func carbonResult(known map[int64]float64, ever float64, byHour map[int64]float64) *carbon.Result {
	hours := make([]carbon.HourKWh, 0, len(byHour))
	for h, k := range byHour {
		hours = append(hours, carbon.HourKWh{Hour: h, KWh: k})
	}
	r := carbon.Value(known, ever, hours)
	return &r
}

// carbonSeries is the house's footprint over time: for each hour (a day, over a long period) the mains' energy at that hour's grid intensity, and the
// intensity itself. Without a whole-house meter, or before any intensity was ever saved, there are no points.
func (s *Server) carbonSeries(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	bucket := model.BucketHour
	if s.defaultBucketFor(r.Context(), from, to) == model.BucketDay {
		bucket = model.BucketDay
	}
	out := model.CarbonSeries{Bucket: bucket, Points: []model.CarbonPoint{}}
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var mains []string
	for _, ch := range on {
		if ch.Scope == model.ScopeMains {
			mains = append(mains, ch.ID)
		}
	}
	ever, ok, err := s.Store.AverageCarbonEver(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if len(mains) == 0 || !ok {
		writeJSON(w, http.StatusOK, out)
		return
	}
	known, err := s.Store.QueryCarbon(r.Context(), from, to)
	if err != nil {
		serverError(w, err)
		return
	}
	byHour, err := s.Store.HourlyKWh(r.Context(), mains, from, to)
	if err != nil {
		serverError(w, err)
		return
	}
	hours := make([]carbon.HourKWh, 0, len(byHour))
	for h, k := range byHour {
		hours = append(hours, carbon.HourKWh{Hour: h, KWh: k})
	}
	startOf := func(hour int64) int64 { return hour }
	if bucket == model.BucketDay {
		startOf = func(hour int64) int64 {
			l := time.Unix(hour, 0).In(s.Loc)
			return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, s.Loc).Unix()
		}
	}
	// The series starts where the house's own record does: grid hours saved before the first mains minute (history that was cut away, say) would only draw a flat line.
	var first time.Time
	for _, id := range mains {
		if t, ok, err := s.Store.FirstMinute(r.Context(), id); err != nil {
			serverError(w, err)
			return
		} else if ok && (first.IsZero() || t.Before(first)) {
			first = t
		}
	}
	avg := carbon.PeriodAverage(known, ever)
	out.GridAverageGramsPerKWh = &avg
	for _, st := range carbon.Bucketed(known, ever, hours, startOf) {
		if !first.IsZero() && st.Start < startOf(first.Unix()/3600*3600) {
			continue
		}
		out.Points = append(out.Points, model.CarbonPoint{T: time.Unix(st.Start, 0).UTC(), KWh: st.KWh, CarbonG: st.Kg * 1000, EstimatedKWh: st.EstimatedKWh, GridGramsPerKWh: st.GridGrams})
	}
	writeJSON(w, http.StatusOK, out)
}
