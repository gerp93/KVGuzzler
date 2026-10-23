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

// carbonOf is the carbon of some channels' energy over [from, to): each hour's kWh at that hour's intensity (see carbon.Value). Nil when no
// intensity has ever been saved. All the channels must be of one scope, as with any sum here.
func (s *Server) carbonOf(ctx context.Context, channelIDs []string, from, to time.Time) (*carbon.Result, error) {
	ever, ok, err := s.Store.AverageCarbonEver(ctx)
	if err != nil || !ok {
		return nil, err
	}
	known, err := s.Store.QueryCarbon(ctx, from, to)
	if err != nil {
		return nil, err
	}
	byHour, err := s.Store.HourlyKWh(ctx, channelIDs, from, to)
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
