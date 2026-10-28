package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/ford"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// The saved pieces of reading the odometer from Ford. The setup (it holds the client secret) and the refresh token are in the private secrets file,
// not the database; what the last read said and the linking attempt in progress are in the meta table.
const (
	fordConfigKey  = "ford_config"
	fordRefreshKey = "ford_refresh"
	fordStatusKey  = "ford_status"
	fordPendingKey = "ford_pending"

	// fordLinkValid is how long a started link can be finished: Ford's own one-time code lasts 10 minutes.
	fordLinkValid = 15 * time.Minute
)

type fordPending struct {
	State string    `json:"state"`
	At    time.Time `json:"at"`
}

func (s *Server) fordClient() *ford.Client {
	if s.Ford != nil {
		return s.Ford
	}
	return &ford.Client{}
}

// FordFeed is the one feed that keeps the odometer up to date; the server's main runs it, and the dashboard's buttons use the same one so two
// refreshes never run at once.
func (s *Server) FordFeed() *ford.Feed {
	s.fordOnce.Do(func() {
		s.fordFeed = &ford.Feed{Client: s.fordClient(), Sink: fordSink{s}, Loc: s.Loc, Now: s.now}
	})
	return s.fordFeed
}

// fordSink lets the Ford feed read its setup and save what it reads in this server.
type fordSink struct{ s *Server }

func (k fordSink) Config(ctx context.Context) (*ford.Config, error) { return k.s.loadFordConfig(ctx) }

func (k fordSink) Refresh(ctx context.Context) (string, bool, error) {
	return k.s.Store.Secret(ctx, fordRefreshKey)
}

func (k fordSink) SaveRefresh(ctx context.Context, token string) error {
	return k.s.Store.SetSecret(ctx, fordRefreshKey, token)
}

func (k fordSink) ClearRefresh(ctx context.Context) error {
	return k.s.Store.SetSecret(ctx, fordRefreshKey, "")
}

func (k fordSink) SaveStatus(ctx context.Context, st ford.Status) error {
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return k.s.Store.SetMeta(ctx, fordStatusKey, string(body))
}

// SaveOdometer writes the day's reading unless the odometer has not moved since the latest one (a parked car would otherwise add a reading a day
// that says nothing). The store's own refusals (an odometer only goes up) are the user's to read, so they go back as Ford errors.
func (k fordSink) SaveOdometer(ctx context.Context, meterID, day string, miles float64) (bool, error) {
	readings, err := k.s.Store.Odometer(ctx, meterID)
	if err != nil {
		return false, err
	}
	if n := len(readings); n > 0 && readings[n-1].Day != day && math.Abs(readings[n-1].Miles-miles) < 0.05 {
		return false, nil
	}
	if err := k.s.Store.SetOdometer(ctx, meterID, day, miles); err != nil {
		if errors.Is(err, store.ErrInvalid) || errors.Is(err, store.ErrNotFound) {
			return false, &ford.Error{Message: "Not saved: " + err.Error()}
		}
		return false, err
	}
	return true, nil
}

// loadFordConfig is the saved setup, or nil when none is set up or what is saved cannot be used.
func (s *Server) loadFordConfig(ctx context.Context) (*ford.Config, error) {
	text, ok, err := s.Store.Secret(ctx, fordConfigKey)
	if err != nil || !ok || text == "" {
		return nil, err
	}
	var c ford.Config
	if json.Unmarshal([]byte(text), &c) != nil || ford.Problem(c, true) != "" {
		return nil, nil
	}
	return &c, nil
}

func (s *Server) fordStatus(ctx context.Context) (*ford.Status, error) {
	text, ok, err := s.Store.Meta(ctx, fordStatusKey)
	if err != nil || !ok || text == "" {
		return nil, err
	}
	var st ford.Status
	if json.Unmarshal([]byte(text), &st) != nil {
		return nil, nil
	}
	return &st, nil
}

func (s *Server) fordView(ctx context.Context) (model.FordLink, error) {
	out := model.FordLink{}
	cfg, err := s.loadFordConfig(ctx)
	if err != nil {
		return out, err
	}
	if cfg != nil {
		out.Configured = true
		out.ClientID, out.AuthorizeURL, out.TokenURL, out.OdometerUnit, out.MeterID = cfg.ClientID, cfg.AuthorizeURL, cfg.TokenURL, cfg.OdometerUnit, cfg.MeterID
		_, out.Linked, err = s.Store.Secret(ctx, fordRefreshKey)
		if err != nil {
			return out, err
		}
	}
	st, err := s.fordStatus(ctx)
	if err != nil || st == nil || !out.Configured {
		return out, err
	}
	checked := st.CheckedAt
	out.LastCheck = &checked
	out.NeedsLink = st.NeedsLink
	if st.OK {
		msg := "Odometer read."
		if !st.Saved {
			msg = "Odometer read; it had not moved since the last reading."
		}
		out.LastOutcome = &msg
		out.LastRaw, out.LastMiles = st.Raw, st.Miles
		if st.Found != "" {
			out.LastFound = &st.Found
		}
		if st.Day != "" {
			out.LastDay = &st.Day
		}
	} else {
		out.LastOutcome = &st.Error
	}
	return out, nil
}

// answerFord answers with the setup and the last outcome, never the secret or a token.
func (s *Server) answerFord(w http.ResponseWriter, r *http.Request) {
	v, err := s.fordView(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// putFord saves the setup. The client secret is write-only: leave it out to keep the saved one. A new client, secret or token address makes the
// saved permission useless, so the vehicle has to be linked again; the account-linking address and the other choices keep it.
func (s *Server) putFord(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID     string  `json:"client_id"`
		ClientSecret *string `json:"client_secret"`
		AuthorizeURL string  `json:"authorize_url"`
		TokenURL     string  `json:"token_url"`
		OdometerUnit string  `json:"odometer_unit"`
		MeterID      string  `json:"meter_id"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_ford", "send client_id, client_secret (the first time), authorize_url, token_url, odometer_unit and meter_id")
		return
	}
	have, err := s.loadFordConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	cfg := ford.Config{
		ClientID:     strings.TrimSpace(body.ClientID),
		AuthorizeURL: strings.TrimSpace(body.AuthorizeURL),
		TokenURL:     strings.TrimSpace(body.TokenURL),
		OdometerUnit: body.OdometerUnit,
		MeterID:      strings.TrimSpace(body.MeterID),
	}
	if body.ClientSecret != nil {
		cfg.ClientSecret = strings.TrimSpace(*body.ClientSecret)
	}
	if cfg.ClientSecret == "" && have != nil {
		cfg.ClientSecret = have.ClientSecret
	}
	if msg := ford.Problem(cfg, false); msg != "" {
		writeProblem(w, http.StatusBadRequest, "bad_ford", msg)
		return
	}
	m, err := s.Store.Meter(r.Context(), cfg.MeterID)
	if err != nil {
		evStatus(w, err)
		return
	}
	if !m.EV {
		writeProblem(w, http.StatusBadRequest, "bad_ford", "That meter is not marked as charging an electric vehicle.")
		return
	}
	text, err := json.Marshal(cfg)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := s.Store.SetSecret(r.Context(), fordConfigKey, string(text)); err != nil {
		serverError(w, err)
		return
	}
	if have != nil && (have.ClientID != cfg.ClientID || have.ClientSecret != cfg.ClientSecret || have.TokenURL != cfg.TokenURL) {
		if err := s.Store.SetSecret(r.Context(), fordRefreshKey, ""); err != nil {
			serverError(w, err)
			return
		}
		_ = s.Store.SetMeta(r.Context(), fordPendingKey, "")
		_ = s.Store.SetMeta(r.Context(), fordStatusKey, "")
	}
	s.answerFord(w, r)
}

// startFordLink makes the address the owner opens to sign in at Ford and agree to share the vehicle, with a fresh state that is checked when the
// owner pastes the address Ford sent the browser to.
func (s *Server) startFordLink(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadFordConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if cfg == nil {
		writeProblem(w, http.StatusConflict, "ford_not_set_up", "Save the Ford client ID, secret and addresses first.")
		return
	}
	state, err := ford.NewState()
	if err != nil {
		serverError(w, err)
		return
	}
	link, err := ford.LinkURL(*cfg, state)
	if err != nil {
		serverError(w, err)
		return
	}
	text, _ := json.Marshal(fordPending{State: state, At: s.now().UTC()})
	if err := s.Store.SetMeta(r.Context(), fordPendingKey, string(text)); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": link, "redirect_uri": ford.RedirectURI(*cfg)})
}

// finishFordLink takes the address the browser was sent to after the owner agreed, trades its code for tokens and reads the odometer once.
func (s *Server) finishFordLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Redirect string `json:"redirect"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_ford", `send the address as {"redirect": "..."}`)
		return
	}
	text, ok, err := s.Store.Meta(r.Context(), fordPendingKey)
	if err != nil {
		serverError(w, err)
		return
	}
	var pending fordPending
	if !ok || text == "" || json.Unmarshal([]byte(text), &pending) != nil || s.now().Sub(pending.At) > fordLinkValid {
		writeProblem(w, http.StatusConflict, "ford_no_link", "No link is waiting: start linking again (Ford's code only lasts 10 minutes).")
		return
	}
	code, err := ford.CodeFromRedirect(body.Redirect, pending.State)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_ford", err.Error())
		return
	}
	// A code is single-use, so the attempt is over whatever happens next.
	_ = s.Store.SetMeta(r.Context(), fordPendingKey, "")
	if _, err := s.FordFeed().Complete(r.Context(), code); err != nil {
		fordFeedError(w, err)
		return
	}
	s.answerFord(w, r)
}

// readFord reads the odometer now, like the daily look does.
func (s *Server) readFord(w http.ResponseWriter, r *http.Request) {
	if _, err := s.FordFeed().Read(r.Context()); err != nil {
		fordFeedError(w, err)
		return
	}
	s.answerFord(w, r)
}

func fordFeedError(w http.ResponseWriter, err error) {
	var fe *ford.Error
	switch {
	case errors.As(err, &fe):
		writeProblem(w, http.StatusBadGateway, "ford_refused", fe.Message)
	case errors.Is(err, ford.ErrNotSetUp):
		writeProblem(w, http.StatusConflict, "ford_not_set_up", "Save the Ford client ID, secret and addresses first.")
	default:
		serverError(w, err)
	}
}

// deleteFord removes the setup, the saved permission and the last outcome. The readings already saved stay.
func (s *Server) deleteFord(w http.ResponseWriter, r *http.Request) {
	for _, k := range []string{fordConfigKey, fordRefreshKey} {
		if err := s.Store.SetSecret(r.Context(), k, ""); err != nil {
			serverError(w, err)
			return
		}
	}
	_ = s.Store.SetMeta(r.Context(), fordPendingKey, "")
	_ = s.Store.SetMeta(r.Context(), fordStatusKey, "")
	w.WriteHeader(http.StatusNoContent)
}
