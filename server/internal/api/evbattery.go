package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
)

// The battery's usable energy in kWh, which turns the car's battery percentages into energy so what charging lost can be worked out (the wall's energy less
// the battery's). Ford's telemetry has a battery capacity member but this car leaves it empty, so the owner says it once, from the car's specification.
// It is the owner's figure and nothing is assumed: until it is set there are no loss figures. Saved per charging meter (`ev_battery:<meter id>` in the meta table).
const (
	evBatteryKey                 = "ev_battery:"
	minBatteryKWh, maxBatteryKWh = 10.0, 300.0
)

type evBatteryBody struct {
	KWh *float64 `json:"kwh"`
}

type evBatteryView struct {
	MeterID string   `json:"meter_id"`
	KWh     *float64 `json:"kwh"`
}

func (s *Server) evBatteryKWh(ctx context.Context, meterID string) (*float64, error) {
	text, ok, err := s.Store.Meta(ctx, evBatteryKey+meterID)
	if err != nil || !ok || text == "" {
		return nil, err
	}
	var b evBatteryBody
	if json.Unmarshal([]byte(text), &b) != nil || b.KWh == nil || !(*b.KWh >= minBatteryKWh && *b.KWh <= maxBatteryKWh) {
		return nil, nil
	}
	return b.KWh, nil
}

func (s *Server) getEvBattery(w http.ResponseWriter, r *http.Request) {
	id, ok := s.evMeterFor(w, r)
	if !ok {
		return
	}
	kwh, err := s.evBatteryKWh(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, evBatteryView{MeterID: id, KWh: kwh})
}

func (s *Server) putEvBattery(w http.ResponseWriter, r *http.Request) {
	id, ok := s.evMeterFor(w, r)
	if !ok {
		return
	}
	var body evBatteryBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.KWh == nil || math.IsNaN(*body.KWh) {
		writeProblem(w, http.StatusBadRequest, "bad_battery", `send the battery's usable energy as {"kwh": number}`)
		return
	}
	if *body.KWh < minBatteryKWh || *body.KWh > maxBatteryKWh {
		writeProblem(w, http.StatusBadRequest, "bad_battery", "The battery's usable energy is a number of kWh between 10 and 300.")
		return
	}
	text, err := json.Marshal(body)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := s.Store.SetMeta(r.Context(), evBatteryKey+id, string(text)); err != nil {
		serverError(w, err)
		return
	}
	s.getEvBattery(w, r)
}

func (s *Server) deleteEvBattery(w http.ResponseWriter, r *http.Request) {
	id, ok := s.evMeterFor(w, r)
	if !ok {
		return
	}
	if err := s.Store.SetMeta(r.Context(), evBatteryKey+id, ""); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
