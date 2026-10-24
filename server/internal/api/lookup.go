package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/openei"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// The saved pieces of the "price from a utility's published tariff" mode. The tariff and profile live in the meta table next to the rate
// configuration; the key is write-only (it is never sent back), goes only to api.openei.org, and is kept in the private secrets file, not the database.
const (
	openEiKeyKey   = "openei_key"
	tariffKey      = "tariff"
	profileKey     = "usage_profile"
	profileAtKey   = "usage_profile_at"
	tariffMaxAge   = 7 * 24 * time.Hour
	profileEvery   = time.Hour
	profileHistory = 60 * 24 * time.Hour
)

var apiKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// savedTariff is the looked-up tariff in use, or nil.
func (s *Server) savedTariff(ctx context.Context) (*rates.Tariff, error) {
	text, ok, err := s.Store.Meta(ctx, tariffKey)
	if err != nil || !ok {
		return nil, err
	}
	return rates.ParseTariff(text), nil
}

func (s *Server) savedProfile(ctx context.Context) (*rates.UsageProfile, error) {
	text, ok, err := s.Store.Meta(ctx, profileKey)
	if err != nil || !ok {
		return nil, err
	}
	return rates.ParseProfile(text), nil
}

func (s *Server) openEiKey(ctx context.Context) (string, error) {
	key, _, err := s.Store.Secret(ctx, openEiKeyKey)
	return key, err
}

func (s *Server) lookup() openei.Lookup {
	if s.Lookup != nil {
		return s.Lookup
	}
	return nil
}

// getOpenEi says whether a key is saved. The key itself is never sent back.
func (s *Server) getOpenEi(w http.ResponseWriter, r *http.Request) {
	key, err := s.openEiKey(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model.OpenEiStatus{HasKey: key != ""})
}

// putOpenEi saves (or, when empty, removes) the OpenEI API key. It is kept in the server's database and sent nowhere but api.openei.org.
func (s *Server) putOpenEi(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"api_key"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_key", "send {\"api_key\": \"...\"}")
		return
	}
	key := strings.TrimSpace(body.APIKey)
	if key != "" && !apiKeyPattern.MatchString(key) {
		writeProblem(w, http.StatusBadRequest, "bad_key", "An OpenEI API key is letters, digits, dashes and underscores, 8 or more of them. Check it was copied whole.")
		return
	}
	if err := s.Store.SetSecret(r.Context(), openEiKeyKey, key); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, model.OpenEiStatus{HasKey: key != ""})
}

func (s *Server) tariffInfo(t *rates.Tariff, currency string) model.TariffInfo {
	return model.TariffInfo{
		Label: t.Label, Utility: t.Utility, Name: t.Name, URI: t.URI, FetchedAt: t.FetchedAt,
		Summary: t.Summary(currencySymbol(currency)), Cheapest: t.Cheapest(), Dearest: t.Dearest(), VariesByTime: t.VariesByTime(),
		Periods: t.Periods, Tiers: t.Tiers, FixedCharge: t.FixedCharge,
	}
}

// currencySymbol is the sign shown before an amount: the common ones, else the three-letter code and a space.
func currencySymbol(code string) string {
	switch code {
	case "USD", "CAD", "AUD", "NZD", "MXN":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "JPY", "CNY":
		return "¥"
	case "INR":
		return "₹"
	case "CHF":
		return "CHF "
	case "":
		return "$"
	}
	return code + " "
}

// lookupRates searches OpenEI for the residential rates around an address or ZIP code.
func (s *Server) lookupRates(w http.ResponseWriter, r *http.Request) {
	address := strings.TrimSpace(r.URL.Query().Get("address"))
	if address == "" || len(address) > 200 {
		writeProblem(w, http.StatusBadRequest, "bad_address", "Type an address or ZIP code to look rates up for.")
		return
	}
	key, err := s.openEiKey(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if key == "" {
		writeProblem(w, http.StatusBadRequest, "no_key", "Save your OpenEI API key first (it is free at openei.org).")
		return
	}
	l := s.lookup()
	if l == nil {
		notImplemented(w, r)
		return
	}
	found, err := l.Find(r.Context(), key, address, s.now())
	if err != nil {
		lookupError(w, err)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	out := model.TariffCandidates{Candidates: []model.TariffCandidate{}}
	for _, c := range found {
		out.Candidates = append(out.Candidates, model.TariffCandidate{TariffInfo: s.tariffInfo(c.Tariff, cfg.Currency), IsDefault: c.IsDefault, Approved: c.Approved})
	}
	writeJSON(w, http.StatusOK, out)
}

// lookupError is an OpenEI problem worded to show as it is (it never carries the key), or a plain failure.
func lookupError(w http.ResponseWriter, err error) {
	var oe *openei.Error
	if errors.As(err, &oe) {
		writeProblem(w, http.StatusBadGateway, "openei", oe.Message)
		return
	}
	serverError(w, err)
}

// getTariff is the looked-up tariff in use, or 204 when none is.
func (s *Server) getTariff(w http.ResponseWriter, r *http.Request) {
	t, err := s.savedTariff(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if t == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.tariffInfo(t, cfg.Currency))
}

// chooseTariff is what saving the lookup mode does: it reads the chosen rate again from OpenEI by its label (the server never trusts a rate
// grid sent by a browser) and keeps it. A tariff already in use under that label is kept as it is.
func (s *Server) chooseTariff(ctx context.Context, label string) (int, string, error) {
	if label == "" {
		return http.StatusBadRequest, "Pick a rate first: look rates up for your address and choose one.", nil
	}
	if have, err := s.savedTariff(ctx); err != nil {
		return 0, "", err
	} else if have != nil && have.Label == label {
		return 0, "", nil
	}
	key, err := s.openEiKey(ctx)
	if err != nil {
		return 0, "", err
	}
	if key == "" {
		return http.StatusBadRequest, "Save your OpenEI API key first (it is free at openei.org).", nil
	}
	l := s.lookup()
	if l == nil {
		return http.StatusNotImplemented, "Looking rates up is not available on this server.", nil
	}
	c, err := l.Get(ctx, key, label, s.now())
	if err != nil {
		var oe *openei.Error
		if errors.As(err, &oe) {
			return http.StatusBadGateway, oe.Message, nil
		}
		return 0, "", err
	}
	if c == nil {
		return http.StatusBadRequest, "OpenEI no longer has that rate. Look rates up again and choose another.", nil
	}
	body, err := json.Marshal(c.Tariff)
	if err != nil {
		return 0, "", err
	}
	return 0, "", s.Store.SetMeta(ctx, tariffKey, string(body))
}

// RefreshRates keeps the looked-up price current: a saved tariff older than a week is read again by its label (the old one stays if that
// fails), and when the tariff varies by time of day the hours the power is used (learned from the last 60 days of the house's history, at
// most hourly) are saved so a day's price is weighted by them. Nothing happens in the other price modes. Errors are returned for the log.
func (s *Server) RefreshRates(ctx context.Context) error {
	cfg, err := s.rateConfig(ctx)
	if err != nil || cfg.Mode != rates.ModeLookup {
		return err
	}
	now := s.now()
	var problem error

	t := cfg.Tariff
	if t != nil && now.Sub(t.FetchedAt) > tariffMaxAge {
		if key, err := s.openEiKey(ctx); err == nil && key != "" && s.lookup() != nil {
			if c, err := s.lookup().Get(ctx, key, t.Label, now); err != nil {
				problem = err
			} else if c != nil {
				if body, err := json.Marshal(c.Tariff); err == nil {
					_ = s.Store.SetMeta(ctx, tariffKey, string(body))
					t = c.Tariff
				}
			}
		}
	}
	if t == nil || !t.VariesByTime() {
		return problem
	}

	if at, ok, err := s.Store.Meta(ctx, profileAtKey); err == nil && ok {
		if when, err := time.Parse(time.RFC3339, at); err == nil && now.Sub(when) < profileEvery {
			return problem
		}
	}
	if err := s.learnProfile(ctx, now); err != nil {
		return err
	}
	return problem
}

// learnProfile saves when the house uses power: the mains clamps' hourly energy over the last 60 days, else the computers' wall plugs.
func (s *Server) learnProfile(ctx context.Context, now time.Time) error {
	on, _, err := s.enabledMeters(ctx)
	if err != nil {
		return err
	}
	var ids []string
	for _, c := range on {
		if c.Scope == model.ScopeMains {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		for _, c := range on {
			if c.Scope == model.ScopePC {
				ids = append(ids, c.ID)
			}
		}
	}
	var hours []rates.HourEnergy
	if len(ids) > 0 {
		byHour, err := s.Store.HourlyKWh(ctx, ids, now.Add(-profileHistory), now)
		if err != nil {
			return err
		}
		for h, k := range byHour {
			hours = append(hours, rates.HourEnergy{Hour: h, KWh: k})
		}
	}
	p := rates.ProfileFromHourly(hours, s.Loc)
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if err := s.Store.SetMeta(ctx, profileKey, string(body)); err != nil {
		return err
	}
	return s.Store.SetMeta(ctx, profileAtKey, now.UTC().Format(time.RFC3339))
}

// RunRatesRefresh calls RefreshRates now and then until ctx ends, reporting problems with logf.
func (s *Server) RunRatesRefresh(ctx context.Context, every time.Duration, logf func(format string, a ...any)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		if err := s.RefreshRates(ctx); err != nil && ctx.Err() == nil {
			logf("refreshing the looked-up price: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
