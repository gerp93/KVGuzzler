package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// The day history starts on (see store/floor.go): a global cutoff the owner can set, so that no period, not even "all time", looks at anything recorded before
// it. Nothing is deleted: taking it off, or moving it earlier, brings the older data back.
type historyStartView struct {
	// Day is the local day history starts on, null when history is not cut.
	Day *string `json:"day"`
	// RecordedSince is the first moment anything was recorded, whatever the cutoff says, so the owner can see what a day would leave out; null when nothing was.
	RecordedSince *time.Time `json:"recorded_since"`
}

func (s *Server) historyStartView(r *http.Request) (historyStartView, error) {
	var out historyStartView
	if day, _, ok := s.Store.HistoryStart(); ok {
		out.Day = &day
	}
	first, ok, err := s.Store.FirstDataEver(r.Context())
	if err != nil {
		return out, err
	}
	if ok {
		out.RecordedSince = &first
	}
	return out, nil
}

func (s *Server) getHistoryStart(w http.ResponseWriter, r *http.Request) {
	v, err := s.historyStartView(r)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) putHistoryStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Day string `json:"day"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Day == "" {
		writeProblem(w, http.StatusBadRequest, "bad_history_start", `send the first day of history as {"day": "2026-10-27"}`)
		return
	}
	start, err := time.ParseInLocation("2006-01-02", body.Day, s.Loc)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_history_start", "The day is a date like 2026-10-27.")
		return
	}
	if start.After(s.now()) {
		writeProblem(w, http.StatusBadRequest, "bad_history_start", "History cannot start in the future.")
		return
	}
	if err := s.Store.SetHistoryStart(r.Context(), body.Day, start); err != nil {
		serverError(w, err)
		return
	}
	s.forgetDataSince()
	s.getHistoryStart(w, r)
}

func (s *Server) deleteHistoryStart(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.SetHistoryStart(r.Context(), "", time.Time{}); err != nil {
		serverError(w, err)
		return
	}
	s.forgetDataSince()
	w.WriteHeader(http.StatusNoContent)
}
