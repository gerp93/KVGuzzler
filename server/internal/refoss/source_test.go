package refoss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The fixtures are what a real EM16P answered (internal/refoss/testdata): A1/B1 +3.75 kW, A2/B2 -3.67 kW (clamped on backwards), A3/B3 a
// few tens of watts, the rest idle, and the C group with no voltage at all.

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTheRealStatusAnswerIsReadIntoItsEighteenInputs(t *testing.T) {
	in, err := ParseStatus(fixture(t, "status-em16p.json"))
	if err != nil || len(in) != 18 {
		t.Fatalf("%d inputs, %v", len(in), err)
	}
	if a2 := in[2]; a2.Power > -3600 || a2.Current < 30 || a2.PF != -1 || a2.Voltage < 118 || a2.Voltage > 119 {
		t.Errorf("A2 = %+v: a load on a clamp put on backwards reads negative", a2)
	}
	if c1 := in[13]; c1.Voltage > 1 {
		t.Errorf("C1 = %+v: nothing is wired to the C phase", c1)
	}
}

func TestOnlyAnAnswerWithInputsIsARefosss(t *testing.T) {
	for _, body := range []string{"", "<html>router</html>", "[]", `{}`, `{"wifi":{"status":"connected"}}`, `{"em:1":{"power":"x"}}`, `{"em:1":{"voltage":120}}`} {
		if _, err := ParseStatus([]byte(body)); err == nil {
			t.Errorf("%q should not be a Refoss's answer", body)
		}
	}
	if in, err := ParseStatus([]byte(`{"em:3":{"power":5,"voltage":120},"em:x":{"power":1,"voltage":1},"em:0":{"power":1,"voltage":1},"sys":{}}`)); err != nil || len(in) != 1 {
		t.Errorf("only em:N objects count: %v %v", in, err)
	}
}

func TestInputsAreNamedAsTheMonitorNamesThem(t *testing.T) {
	for n, want := range map[int]string{1: "A1", 2: "A2", 6: "A6", 7: "B1", 8: "B2", 12: "B6", 13: "C1", 18: "C6"} {
		if got := InputLabel(n); got != want {
			t.Errorf("input %d = %s, want %s", n, got, want)
		}
	}
}

func TestOnlyTheEm16pIsSupported(t *testing.T) {
	if !SupportedModel("em16p") || !SupportedModel("EM16P") || SupportedModel("em06p") || SupportedModel("") {
		t.Error("supported models")
	}
	if _, err := NewSource("192.168.68.88", "", "em06p", nil, nil); err == nil || !strings.Contains(err.Error(), "EM06P") {
		t.Errorf("an EM06P: %v", err)
	}
	if _, err := NewSource("http://192.168.68.88/x", "", "em16p", nil, nil); err == nil {
		t.Error("a URL is not a host")
	}
}

func source(t *testing.T, status string, settings map[string]Setting) *Source {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rpc/Refoss.Status.Get" {
			t.Errorf("unexpected request %s %s: this source only reads Status.Get", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(status))
	}))
	t.Cleanup(srv.Close)
	return NewSourceWithBase(srv.URL, "192.168.68.88", "", "em16p", func(context.Context) (map[string]Setting, error) { return settings, nil }, srv.Client())
}

func poll(t *testing.T, s *Source) map[string]float64 {
	t.Helper()
	rs, err := s.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, r := range rs {
		out[r.MeterID] = r.Watts
	}
	return out
}

const dev = "refoss-192.168.68.88:em"

func TestAMonitorHasEighteenMetersAndOnlyReadsStatus(t *testing.T) {
	s := source(t, string(fixture(t, "status-em16p.json")), nil)
	d := s.Device()
	if d.ID != "refoss-192.168.68.88" || d.Kind != "refoss" || d.Name != "Refoss EM16P" || d.Model != "em16p" {
		t.Errorf("device = %+v", d)
	}
	chs := s.Meters()
	if len(chs) != 18 || chs[0].ID != dev+"1" || chs[0].Label != "A1" || chs[7].Label != "B2" || chs[17].Label != "C6" || chs[0].Scope != "circuit" || chs[0].MaxWatts == nil {
		t.Errorf("meters = %+v", chs)
	}
	for _, ch := range chs {
		if ch.DeviceID != d.ID || !strings.HasPrefix(ch.ID, d.ID+":") {
			t.Errorf("meter %+v is not under its device", ch)
		}
	}
}

func TestWithNoSettingsABackwardsClampReportsNothingAndSaysSo(t *testing.T) {
	s := source(t, string(fixture(t, "status-em16p.json")), nil)
	got := poll(t, s)
	if got[dev+"1"] < 3700 || got[dev+"7"] < 3700 || got[dev+"3"] < 18 || got[dev+"9"] < 49 {
		t.Errorf("readings = %v", got)
	}
	if _, ok := got[dev+"2"]; ok {
		t.Errorf("A2 reported %v W although it reads backwards", got[dev+"2"])
	}
	if _, ok := got[dev+"13"]; ok {
		t.Error("C1 has no voltage and should report nothing")
	}
	if w, ok := got[dev+"4"]; !ok || w != 0 {
		t.Errorf("A4 is wired and idle: %v %v", w, ok) // an idle input reports a true zero
	}
	notes := strings.Join(s.Notes(), "\n")
	if !strings.Contains(notes, "A2 on the Refoss reads -3668") || !strings.Contains(notes, "B2 on the Refoss reads -3666") || !strings.Contains(notes, "Flip sign") {
		t.Errorf("notes = %q", notes)
	}
	active := strings.Join(s.Active(), ",")
	for _, n := range []string{"1", "2", "3", "7", "8", "9"} {
		if !strings.Contains(active+",", dev+n+",") {
			t.Errorf("input %s has a load and should be active: %s", n, active)
		}
	}
	if strings.Contains(active, dev+"4,") || strings.Contains(active, dev+"13") {
		t.Errorf("idle inputs are active: %s", active)
	}
}

func TestFlippingTheSignReadsTheRealPowerAndAPairIsBothLegsAddedAndOneMeter(t *testing.T) {
	flip := map[string]Setting{dev + "2": {Invert: true}, dev + "8": {Invert: true}}
	s := source(t, string(fixture(t, "status-em16p.json")), flip)
	got := poll(t, s)
	if w := got[dev+"2"]; w < 3660 || w > 3680 {
		t.Errorf("flipped A2 = %v", got[dev+"2"])
	}
	if len(s.Notes()) != 0 {
		t.Errorf("nothing is backwards now: %v", s.Notes())
	}

	paired := map[string]Setting{dev + "2": {Invert: true, PairWith: dev + "8"}, dev + "8": {Invert: true}}
	s = source(t, string(fixture(t, "status-em16p.json")), paired)
	got = poll(t, s)
	if w := got[dev+"2"]; w < 7300 || w > 7400 {
		t.Errorf("the EV circuit = %v, want both legs added (about 7.3 kW)", w)
	}
	if _, ok := got[dev+"8"]; ok {
		t.Error("the second leg is reported on its own as well")
	}
	// Pairing one leg with a leg that has nothing wired to it adds nothing and breaks nothing.
	lonely := map[string]Setting{dev + "1": {PairWith: dev + "13"}}
	got = poll(t, source(t, string(fixture(t, "status-em16p.json")), lonely))
	if w := got[dev+"1"]; w < 3700 || w > 3800 {
		t.Errorf("A1 paired with a leg with no voltage = %v", w)
	}
}

func TestANegativeReadingThatIsOnlyNoiseIsZeroAndAWrongAnswerIsAnError(t *testing.T) {
	noise := `{"em:1":{"current":0.0,"voltage":118,"power":-0.4,"pf":0}}`
	s := source(t, noise, nil)
	if got := poll(t, s); len(got) != 0 || len(s.Notes()) != 0 {
		t.Errorf("noise reported as %v with notes %v", got, s.Notes())
	}
	bad := source(t, `<html>nope</html>`, nil)
	if _, err := bad.Poll(context.Background()); err == nil {
		t.Error("a page that is not a Refoss's answer must be an error")
	}
}

func TestAProblemReadingTheSettingsDoesNotStopTheReadings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(fixture(t, "status-em16p.json")) }))
	t.Cleanup(srv.Close)
	s := NewSourceWithBase(srv.URL, "192.168.68.88", "", "em16p", func(context.Context) (map[string]Setting, error) { return nil, context.DeadlineExceeded }, srv.Client())
	if got := poll(t, s); got[dev+"1"] < 3700 {
		t.Errorf("readings = %v", got)
	}
}

// pollAll is poll, keeping each reading whole.
func pollAll(t *testing.T, s *Source) map[string]struct{ Volts, Amps *float64 } {
	t.Helper()
	rs, err := s.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]struct{ Volts, Amps *float64 }{}
	for _, r := range rs {
		out[r.MeterID] = struct{ Volts, Amps *float64 }{r.Volts, r.Amps}
	}
	return out
}

func TestAReadingCarriesItsVoltsAndAmpsAndAPairKeepsTheLargerLegsCurrent(t *testing.T) {
	status := `{"em:1":{"current":25.4,"voltage":118.2,"power":3000,"pf":1},"em:7":{"current":25.0,"voltage":118.6,"power":2950,"pf":1},"em:2":{"current":3.0,"voltage":118.2,"power":-350,"pf":1}}`
	one := pollAll(t, source(t, status, nil))
	if r := one[dev+"1"]; r.Amps == nil || *r.Amps != 25.4 || r.Volts == nil || *r.Volts != 118.2 {
		t.Errorf("A1 = %+v, want its own 25.4 A at 118.2 V", r)
	}
	// A clamp's current is never negative, whichever way round the clamp is.
	if r := pollAll(t, source(t, status, map[string]Setting{dev + "2": {Invert: true}}))[dev+"2"]; r.Amps == nil || *r.Amps != 3 {
		t.Errorf("flipped A2 = %+v", r)
	}

	// One 240 V breaker carries the same current in both legs: the pair's amps are the larger leg's (25.4), never the sum (50.4);
	// its voltage is the two legs added, about 237 V.
	pair := pollAll(t, source(t, status, map[string]Setting{dev + "1": {PairWith: dev + "7"}}))
	r := pair[dev+"1"]
	if r.Amps == nil || *r.Amps != 25.4 {
		t.Errorf("the pair's amps = %v, want 25.4 (the larger leg), not the 50.4 the legs add to", r.Amps)
	}
	if r.Volts == nil || *r.Volts < 236.7 || *r.Volts > 236.9 {
		t.Errorf("the pair's volts = %v, want the two legs added (236.8)", r.Volts)
	}
	if _, ok := pair[dev+"7"]; ok {
		t.Error("the second leg is reported on its own")
	}
}
