package gasprice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A real answer's shape (from EIA's weekly survey, Minnesota), with the rows in the order EIA gave them for a descending sort.
const answer = `{"response":{"total":"3","dateFormat":"YYYY-MM-DD","frequency":"weekly","data":[
 {"period":"2026-10-26","duoarea":"SMN","product":"EPMR","value":"4.146","units":"$/GAL"},
 {"period":"2026-10-19","duoarea":"SMN","product":"EPMR","value":"4.18","units":"$/GAL"},
 {"period":"2026-10-12","duoarea":"SMN","product":"EPMR","value":"4.184","units":"$/GAL"}]}}`

func TestParsePricesKeepsExactDecimalsInWeekOrder(t *testing.T) {
	got, err := ParsePrices([]byte(answer))
	if err != nil {
		t.Fatal(err)
	}
	want := []Price{{"2026-10-12", "4.184"}, {"2026-10-19", "4.18"}, {"2026-10-26", "4.146"}}
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i].Week != want[i].Week || got[i].Dollars != want[i].Dollars+map[bool]string{true: "0", false: ""}[len(want[i].Dollars) == 3] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParsePricesLeavesOutWhatIsNotAPrice(t *testing.T) {
	got, err := ParsePrices([]byte(`{"response":{"data":[
	 {"period":"2026-10-26","value":"4.1"},
	 {"period":"2026-10-19","value":null},
	 {"period":"2026-10-12","value":"-1"},
	 {"period":"2026-10-05","value":"0"},
	 {"period":"2026-09-28","value":"412"},
	 {"period":"not a day","value":"4.0"},
	 {"period":"2026-09-21","value":"abc"},
	 {"period":"2026-09-14","value":3.9}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Week != "2026-09-14" || got[0].Dollars != "3.90" || got[1].Week != "2026-10-26" {
		t.Fatalf("got %v, want only the two believable weeks", got)
	}
}

func TestParsePricesRefusesWhatIsNotJSON(t *testing.T) {
	if _, err := ParsePrices([]byte("<html>")); err == nil {
		t.Fatal("read an HTML page as prices")
	}
}

func TestWeeklyAsksForRegularGasolineOfTheAreaFromADayAndNeverShowsTheKey(t *testing.T) {
	const key = "SECRET-KEY-12345"
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawQuery
		if r.URL.Query().Get("api_key") != key {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"API_KEY_INVALID: ` + r.URL.Query().Get("api_key") + `"}`))
			return
		}
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL}
	got, err := c.Weekly(context.Background(), key, "SMN", Gasoline, time.Date(2025, 10, 30, 0, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 3 {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, want := range []string{"facets%5Bproduct%5D%5B%5D=EPMR", "facets%5Bduoarea%5D%5B%5D=SMN", "frequency=weekly", "start=2025-10-30"} {
		if !strings.Contains(seen, want) {
			t.Errorf("the request %q lacks %q", seen, want)
		}
	}

	// A key EIA refuses: said without the key, even though EIA echoed it.
	_, err = c.Weekly(context.Background(), "WRONG-KEY-XXXXX", "SMN", Gasoline, time.Now())
	if err == nil || strings.Contains(err.Error(), "WRONG-KEY-XXXXX") || !strings.Contains(err.Error(), "did not accept the key") {
		t.Errorf("a refused key: %v", err)
	}
}

func TestWeeklyAsksForTheFuelItIsGiven(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawQuery
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	if _, err := (&Client{Base: srv.URL}).Weekly(context.Background(), "KEY-KEY-KEY", "SMN", Diesel, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, "facets%5Bproduct%5D%5B%5D=EPD2D") || strings.Contains(seen, "EPMR") {
		t.Errorf("the request %q is not for No. 2 diesel alone", seen)
	}
}

func TestWeeklyRefusesAnAreaEIAHasNoPriceFor(t *testing.T) {
	c := &Client{Base: "http://127.0.0.1:1"} // never reached
	if _, err := c.Weekly(context.Background(), "KEY-KEY-KEY", "ZZ", Gasoline, time.Now()); err == nil || !strings.Contains(err.Error(), "no fuel price") {
		t.Errorf("an unknown area: %v", err)
	}
}

func TestWeeklySaysSoWhenEIAHasNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"response":{"data":[]}}`)) }))
	defer srv.Close()
	if _, err := (&Client{Base: srv.URL}).Weekly(context.Background(), "KEY-KEY-KEY", "SMN", Gasoline, time.Now()); err == nil {
		t.Error("an empty answer was taken as prices")
	}
}

func TestEveryAreaHasANameAndAUniqueCode(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range Areas {
		if a.ID == "" || a.Name == "" || seen[a.ID] {
			t.Errorf("area %+v is empty or repeated", a)
		}
		seen[a.ID] = true
	}
	if name, ok := AreaName("SMN"); !ok || name != "Minnesota" {
		t.Errorf("SMN = %q, %v", name, ok)
	}
}
