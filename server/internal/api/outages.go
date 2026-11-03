package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/outage"
	"github.com/gerp93/KVGuzzler/server/internal/source"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// The outage log: stretches when nothing in the house was recorded, found by the server when it starts again (see package outage) or typed in. An outage
// describes the history and changes no figure in it. The newest few hundred are listed; the totals are over those.
const (
	outageListLimit = 200
	// maxOutage is the longest stretch accepted when one is typed in: a month away is a different thing from an outage.
	maxOutage     = 31 * 24 * time.Hour
	maxOutageNote = 200
)

type outageView struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Seconds   int64     `json:"seconds"`
	Source    string    `json:"source"`
	Note      string    `json:"note"`
}

type outagesAnswer struct {
	Outages []outageView `json:"outages"`
	// PowerSeconds, ServerSeconds and UnknownSeconds add up the listed outages of each kind.
	PowerSeconds   int64 `json:"power_seconds"`
	ServerSeconds  int64 `json:"server_seconds"`
	UnknownSeconds int64 `json:"unknown_seconds"`
	// LeewaySeconds is how long after the power returned the server may take to be back before the time is listed as "server off" on its own.
	LeewaySeconds int64 `json:"leeway_seconds"`
}

func viewOf(o store.Outage) outageView {
	return outageView{ID: o.ID, Kind: o.Kind, StartedAt: o.Started, EndedAt: o.Ended, Seconds: int64(o.Ended.Sub(o.Started).Seconds()), Source: o.Source, Note: o.Note}
}

func (s *Server) listOutages(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Outages(r.Context(), time.Unix(0, 0), outageListLimit)
	if err != nil {
		serverError(w, err)
		return
	}
	out := outagesAnswer{Outages: make([]outageView, 0, len(list)), LeewaySeconds: int64(outage.Leeway.Seconds())}
	for _, o := range list {
		v := viewOf(o)
		out.Outages = append(out.Outages, v)
		switch o.Kind {
		case store.OutagePower:
			out.PowerSeconds += v.Seconds
		case store.OutageServer:
			out.ServerSeconds += v.Seconds
		default:
			out.UnknownSeconds += v.Seconds
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type outageBody struct {
	Kind      *string    `json:"kind"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Note      *string    `json:"note"`
}

func (s *Server) readOutageBody(w http.ResponseWriter, r *http.Request) (outageBody, bool) {
	var b outageBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_outage", `send {"kind": "power", "started_at": "2026-11-02T13:14:00Z", "ended_at": "2026-11-02T14:47:00Z", "note": ""}`)
		return b, false
	}
	return b, true
}

// checkOutage reports why an outage cannot be saved as it is, "" when it can.
func (s *Server) checkOutage(o store.Outage) string {
	switch o.Kind {
	case store.OutagePower, store.OutageServer, store.OutageUnknown:
	default:
		return "The kind is power, server or unknown."
	}
	if !o.Ended.After(o.Started) {
		return "The end has to be after the start."
	}
	if o.Ended.After(s.now().Add(time.Minute)) {
		return "An outage cannot end in the future."
	}
	if o.Ended.Sub(o.Started) > maxOutage {
		return "An outage is at most a month long."
	}
	if len(o.Note) > maxOutageNote {
		return "The note is at most " + strconv.Itoa(maxOutageNote) + " characters."
	}
	return ""
}

func (s *Server) saveOutageProblem(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrOutageOverlap) {
		writeProblem(w, http.StatusConflict, "outage_overlaps", "That overlaps another outage in the log.")
		return
	}
	serverError(w, err)
}

func (s *Server) addOutage(w http.ResponseWriter, r *http.Request) {
	b, ok := s.readOutageBody(w, r)
	if !ok {
		return
	}
	if b.Kind == nil || b.StartedAt == nil || b.EndedAt == nil {
		writeProblem(w, http.StatusBadRequest, "bad_outage", "An outage needs a kind, a start and an end.")
		return
	}
	o := store.Outage{Kind: *b.Kind, Started: b.StartedAt.UTC().Truncate(time.Second), Ended: b.EndedAt.UTC().Truncate(time.Second), Source: store.OutageManual}
	if b.Note != nil {
		o.Note = strings.TrimSpace(*b.Note)
	}
	if msg := s.checkOutage(o); msg != "" {
		writeProblem(w, http.StatusBadRequest, "bad_outage", msg)
		return
	}
	saved, err := s.Store.AddOutage(r.Context(), o)
	if err != nil {
		s.saveOutageProblem(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, viewOf(saved))
}

func (s *Server) updateOutage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("outageId"), 10, 64)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "no_such_outage", "There is no such outage.")
		return
	}
	cur, found, err := s.Store.Outage(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	if !found {
		writeProblem(w, http.StatusNotFound, "no_such_outage", "There is no such outage.")
		return
	}
	b, ok := s.readOutageBody(w, r)
	if !ok {
		return
	}
	if b.Kind != nil {
		cur.Kind = *b.Kind
	}
	if b.StartedAt != nil {
		cur.Started = b.StartedAt.UTC().Truncate(time.Second)
	}
	if b.EndedAt != nil {
		cur.Ended = b.EndedAt.UTC().Truncate(time.Second)
	}
	if b.Note != nil {
		cur.Note = strings.TrimSpace(*b.Note)
	}
	// Whatever the server found, once it is corrected by hand it is the owner's.
	cur.Source = store.OutageManual
	if msg := s.checkOutage(cur); msg != "" {
		writeProblem(w, http.StatusBadRequest, "bad_outage", msg)
		return
	}
	if found, err = s.Store.UpdateOutage(r.Context(), cur); err != nil {
		s.saveOutageProblem(w, err)
		return
	}
	if !found {
		writeProblem(w, http.StatusNotFound, "no_such_outage", "There is no such outage.")
		return
	}
	writeJSON(w, http.StatusOK, viewOf(cur))
}

func (s *Server) deleteOutage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("outageId"), 10, 64)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "no_such_outage", "There is no such outage.")
		return
	}
	found, err := s.Store.DeleteOutage(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	if !found {
		writeProblem(w, http.StatusNotFound, "no_such_outage", "There is no such outage.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunOutageWatch notes that the server is running and, when it was away just before, works out why from when the plugs last started (see package outage). It
// returns when ctx ends.
func (s *Server) RunOutageWatch(ctx context.Context, logf func(format string, a ...any)) {
	w := &outage.Watcher{Store: s.Store, Now: s.now}
	if s.Collector != nil {
		w.Boots = s.Collector.Boots
	} else {
		w.Boots = func(context.Context) []source.Boot { return nil }
	}
	w.Run(ctx, logf)
}
