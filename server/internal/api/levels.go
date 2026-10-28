package api

import (
	"net/http"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/runs"
)

// levelsLookback is how much history the resting and working levels are learned from (the same as the runs' level).
const levelsLookback = thresholdLookback

// levelsFresh is how long the learned levels are kept before they are worked out again: they move over weeks, and the table asks every few minutes.
const levelsFresh = 10 * time.Minute

type meterLevels struct {
	MeterID string `json:"meter_id"`
	// RestWatts is what the meter usually draws while resting.
	RestWatts float64 `json:"rest_watts"`
	// WorkingAboveWatts is the level above which it is working; null for a meter that barely changes (it is on, it does not work).
	WorkingAboveWatts *float64 `json:"working_above_watts"`
	Minutes           int      `json:"minutes"`
}

type levelsAnswer struct {
	Meters []meterLevels `json:"meters"`
	From   time.Time     `json:"from"`
	To     time.Time     `json:"to"`
}

// levels says, for every meter that has enough history, what it draws at rest and what level counts as working, so a table of live readings can
// show which meters are working right now. The same level the runs use. Each meter's own, never added to anything.
func (s *Server) levels(w http.ResponseWriter, r *http.Request) {
	s.levelsMu.Lock()
	defer s.levelsMu.Unlock()
	if s.levelsAnswer != nil && s.now().Sub(s.levelsAt) < levelsFresh {
		writeJSON(w, http.StatusOK, s.levelsAnswer)
		return
	}
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	to := s.now().UTC().Truncate(time.Minute)
	from := to.Add(-levelsLookback)
	out := &levelsAnswer{Meters: []meterLevels{}, From: from, To: to}
	for _, m := range on {
		points, err := s.Store.MeterMinutes(r.Context(), m.ID, from, to)
		if err != nil {
			serverError(w, err)
			return
		}
		l, ok := runs.LevelsOf(points)
		if !ok {
			continue
		}
		ml := meterLevels{MeterID: m.ID, RestWatts: l.RestWatts, Minutes: l.Minutes}
		if l.HasWorking {
			t := l.WorkingAboveWatts
			ml.WorkingAboveWatts = &t
		}
		out.Meters = append(out.Meters, ml)
	}
	s.levelsAnswer, s.levelsAt = out, s.now()
	writeJSON(w, http.StatusOK, out)
}
