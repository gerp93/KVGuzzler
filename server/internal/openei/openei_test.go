package openei

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These are the cases of OpenEiParsingTests and OpenEiClientTests in the C# test project, one for one.

var now = time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC)

// hourTable is a 12 by 24 schedule: the hours peakHour says use period 1, everything else period 0.
func hourTable(peakHour func(int) bool) string {
	months := make([]string, 12)
	for m := range months {
		hours := make([]string, 24)
		for h := range hours {
			hours[h] = "0"
			if peakHour(h) {
				hours[h] = "1"
			}
		}
		months[m] = "[" + strings.Join(hours, ",") + "]"
	}
	return "[" + strings.Join(months, ",") + "]"
}

// timeOfUse is a residential time-of-use rate: 8 cents off-peak, 20 cents from 5pm to 8pm on weekdays (weekends all off-peak), with a
// two-tier first period, a 1 cent adjustment and a fixed charge.
func timeOfUse(label string) string {
	return fmt.Sprintf(`{"label":%q,"utility":"Example Electric","name":"Residential Time of Use","uri":"https://openei.org/apps/USURDB/rate/view/%s",
 "sector":"Residential","servicetype":"Bundled","approved":true,"is_default":false,"startdate":1700000000,
 "fixedchargefirstmeter":6.75,"fixedchargeunits":"$/month",
 "energyratestructure":[[{"rate":0.07,"adj":0.01,"unit":"kWh","max":500},{"rate":0.09,"adj":0.01,"unit":"kWh"}],[{"rate":0.19,"adj":0.01,"unit":"kWh"}]],
 "energyweekdayschedule":%s,
 "energyweekendschedule":%s}`, label, label, hourTable(func(h int) bool { return h >= 17 && h < 20 }), hourTable(func(int) bool { return false }))
}

func flatRate(label string, rate float64, extra string) string {
	return fmt.Sprintf(`{"label":%q,"utility":"Example Electric","name":"Residential Standard","sector":"Residential","servicetype":"Bundled","is_default":true,
 "energyratestructure":[[{"rate":%s,"unit":"kWh"}]]%s}`, label, strconv.FormatFloat(rate, 'f', -1, 64), extra)
}

func items(list ...string) string {
	if len(list) == 0 {
		list = []string{timeOfUse("tou-1"), flatRate("flat-1", 0.12, "")}
	}
	return `{"items":[` + strings.Join(list, ",") + `]}`
}

func parse(t *testing.T, list ...string) []Candidate {
	t.Helper()
	got, err := ParseRates(items(list...), now, true)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestATimeOfUseRateBecomesAPriceForEveryMonthDayKindAndHour(t *testing.T) {
	got := parse(t, timeOfUse("tou-1"))
	if len(got) != 1 {
		t.Fatalf("%d rates", len(got))
	}
	c := got[0]
	tf := c.Tariff
	if tf.Label != "tou-1" || tf.Utility != "Example Electric" || tf.Periods != 2 || tf.Tiers != 2 || !c.Approved || c.IsDefault {
		t.Errorf("tariff = %+v", tf)
	}
	if tf.FixedCharge == nil || *tf.FixedCharge != "6.75 $/month" {
		t.Errorf("fixed charge = %v", tf.FixedCharge)
	}
	if !near(tf.Price(0, false, 12), 0.08) { // off-peak: 0.07 + 0.01, the first tier
		t.Errorf("off-peak = %v", tf.Price(0, false, 12))
	}
	if !near(tf.Price(5, false, 18), 0.20) { // peak: 0.19 + 0.01
		t.Errorf("peak = %v", tf.Price(5, false, 18))
	}
	if !near(tf.Price(5, true, 18), 0.08) { // weekends are all off-peak
		t.Errorf("weekend = %v", tf.Price(5, true, 18))
	}
	if !tf.VariesByTime() || !near(tf.Cheapest(), 0.08) || !near(tf.Dearest(), 0.20) {
		t.Errorf("varies %v, %v to %v", tf.VariesByTime(), tf.Cheapest(), tf.Dearest())
	}
}

func TestAFlatRateNeedsNoSchedule(t *testing.T) {
	tf := parse(t, flatRate("flat-1", 0.1234, ""))[0].Tariff
	if tf.VariesByTime() || !near(tf.Price(7, true, 3), 0.1234) || tf.Periods != 1 {
		t.Errorf("tariff = %+v", tf)
	}
	if !strings.Contains(tf.Summary("$"), "$0.1234 per kWh") {
		t.Errorf("summary = %q", tf.Summary("$"))
	}
}

func TestTheMonthsFuelAdjustmentIsAddedAndNumbersMayBeText(t *testing.T) {
	fuel := `,"fueladjustmentsmonthly":[0.01,0,0,0,0,0,0,0,0,0,0,0.02]`
	tf := parse(t, flatRate("flat-1", 0.10, fuel))[0].Tariff
	if !near(tf.Price(0, false, 0), 0.11) || !near(tf.Price(5, false, 0), 0.10) || !near(tf.Price(11, true, 23), 0.12) {
		t.Errorf("prices = %v %v %v", tf.Price(0, false, 0), tf.Price(5, false, 0), tf.Price(11, true, 23))
	}
	text := parse(t, `{"label":"x","energyratestructure":[[{"rate":"0.15","unit":"kWh"}]]}`)[0].Tariff
	if !near(text.Price(0, false, 0), 0.15) {
		t.Errorf("a price written as text = %v", text.Price(0, false, 0))
	}
}

func TestARateThatCannotBePricedPerKwhIsLeftOutNotGuessedAt(t *testing.T) {
	for name, item := range map[string]string{
		"not residential": `{"label":"a","sector":"Commercial","energyratestructure":[[{"rate":0.1}]]}`,
		"delivery only":   `{"label":"a","servicetype":"Delivery","energyratestructure":[[{"rate":0.1}]]}`,
		"expired":         `{"label":"a","enddate":1700000000,"energyratestructure":[[{"rate":0.1}]]}`,
		"not yet":         `{"label":"a","startdate":1999999999,"energyratestructure":[[{"rate":0.1}]]}`,
		"demand based":    `{"label":"a","energyratestructure":[[{"rate":0.1,"unit":"kWh/kW"}]]}`,
		"megawatt hours":  `{"label":"a","energyratestructure":[[{"rate":0.1,"unit":"MWh"}]]}`,
		"empty structure": `{"label":"a","energyratestructure":[]}`,
		"empty period":    `{"label":"a","energyratestructure":[[]]}`,
		"no price":        `{"label":"a","energyratestructure":[[{"unit":"kWh"}]]}`,
		"no schedule":     `{"label":"a","energyratestructure":[[{"rate":0.1}],[{"rate":0.2}]]}`,
		"negative":        `{"label":"a","energyratestructure":[[{"rate":-1}]]}`,
		"not a household": `{"label":"a","energyratestructure":[[{"rate":900}]]}`,
		"no label":        `{"energyratestructure":[[{"rate":0.1}]]}`,
	} {
		if got := parse(t, item); len(got) != 0 {
			t.Errorf("%s: %d rate(s), want none", name, len(got))
		}
	}
}

func TestAScheduleThatPointsAtAMissingPeriodLeavesTheRateOut(t *testing.T) {
	bad := strings.ReplaceAll(hourTable(func(int) bool { return true }), "1", "5") // period 5 of 2
	item := `{"label":"a","energyratestructure":[[{"rate":0.1}],[{"rate":0.2}]],"energyweekdayschedule":` + bad + `}`
	if got := parse(t, item); len(got) != 0 {
		t.Errorf("%d rate(s), want none", len(got))
	}
}

func TestARateWithoutAWeekendScheduleUsesItsWeekdayOne(t *testing.T) {
	item := `{"label":"a","energyratestructure":[[{"rate":0.1}],[{"rate":0.3}]],"energyweekdayschedule":` + hourTable(func(h int) bool { return h == 18 }) + `}`
	if tf := parse(t, item)[0].Tariff; !near(tf.Price(2, true, 18), 0.3) {
		t.Errorf("weekend 18:00 = %v", tf.Price(2, true, 18))
	}
}

func TestAnythingThatIsNotAListOfRatesIsAMessage(t *testing.T) {
	for _, answer := range []string{"", "<html>busy</html>", "[]", `{"items":5}`, `{"nothing":true}`} {
		_, err := ParseRates(answer, now, true)
		if err == nil || !strings.Contains(err.Error(), "not with a list of rates") {
			t.Errorf("%q: err = %v", answer, err)
		}
	}
}

func TestAnErrorFromOpenEiIsPassedOn(t *testing.T) {
	_, err := ParseRates(`{"error":"Invalid api key"}`, now, true)
	if err == nil || !strings.Contains(err.Error(), "OpenEI said: Invalid api key") {
		t.Errorf("err = %v", err)
	}
}

func TestAnEmptyListIsNoRatesNotAnError(t *testing.T) {
	got, err := ParseRates(`{"items":[]}`, now, true)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestARateKnownByItsLabelIsUsedEvenIfItsDatesHavePassed(t *testing.T) {
	expired := `{"label":"old","enddate":1700000000,"energyratestructure":[[{"rate":0.1}]]}`
	if got, _ := ParseRates(items(expired), now, true); len(got) != 0 {
		t.Errorf("a current search found an expired rate: %v", got)
	}
	if got, _ := ParseRates(items(expired), now, false); len(got) != 1 {
		t.Errorf("a search by label should use it: %v", got)
	}
}

func TestTheAddressAndKeyAreEncodedSoNothingTypedCanChangeTheRequest(t *testing.T) {
	raw := SearchURL(Endpoint, "key123&x=1", "80202 & evil=1/../", now)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "api.openei.org" || u.Path != "/utility_rates" {
		t.Errorf("went to %s%s", u.Host, u.Path)
	}
	q := u.Query()
	if q.Get("api_key") != "key123&x=1" || q.Get("address") != "80202 & evil=1/../" || q.Get("x") != "" || q.Get("evil") != "" {
		t.Errorf("query = %v", q)
	}
	if q.Get("sector") != "Residential" || q.Get("effective_on_date") != strconv.FormatInt(now.Unix(), 10) {
		t.Errorf("query = %v", q)
	}
	if !strings.Contains(u.RawQuery, "api_key=key123%26x%3D1") || strings.Contains(u.RawQuery, "evil=1&") {
		t.Errorf("raw query = %s", u.RawQuery)
	}
	if got := url.Values(mustQuery(t, LabelURL(Endpoint, "k", "a b&c"))).Get("getpage"); got != "a b&c" {
		t.Errorf("getpage = %q", got)
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func fake(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*Client, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		respond(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL + "/utility_rates"}, &seen
}

func ok(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, items()) }

func TestFindsRatesWithAGetAndPutsTheMostCommonFirst(t *testing.T) {
	c, seen := fake(t, ok)
	got, err := c.Find(context.Background(), "abcdefghijklmnop", "80202", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0].Method != http.MethodGet {
		t.Fatalf("requests = %v", *seen)
	}
	if len(got) != 2 || got[0].Tariff.Label != "flat-1" || got[1].Tariff.Label != "tou-1" { // the flat one is marked most common
		t.Errorf("order = %v, %v", got[0].Tariff.Label, got[1].Tariff.Label)
	}
	if !got[0].Tariff.FetchedAt.Equal(now) {
		t.Errorf("fetched at %v", got[0].Tariff.FetchedAt)
	}
}

func TestGetsOneRateByItsLabelOrNilWhenItIsGone(t *testing.T) {
	c, _ := fake(t, ok)
	if got, err := c.Get(context.Background(), "abcdefghijklmnop", "tou-1", now); err != nil || got == nil || got.Tariff.Label != "tou-1" {
		t.Errorf("tou-1 = %v, %v", got, err)
	}
	if got, err := c.Get(context.Background(), "abcdefghijklmnop", "gone", now); err != nil || got != nil {
		t.Errorf("gone = %v, %v", got, err)
	}
}

func TestExplainsWhatTheErrorMeans(t *testing.T) {
	for status, want := range map[int]string{
		400: "did not accept the request", 401: "did not accept the API key", 403: "did not accept the API key",
		429: "slow down", 500: "error (500)",
	} {
		c, _ := fake(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		_, err := c.Find(context.Background(), "abcdefghijklmnop", "80202", now)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%d: err = %v, want %q", status, err, want)
		}
	}
}

func TestAnUnreachableServiceSaysSoAndNeverShowsTheKey(t *testing.T) {
	c := &Client{Base: "http://127.0.0.1:1/utility_rates"} // nothing listens there
	_, err := c.Find(context.Background(), "SECRETKEY123", "80202", now)
	if err == nil || !strings.HasPrefix(err.Error(), "Could not reach OpenEI") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "SECRETKEY123") {
		t.Errorf("the error shows the key: %v", err)
	}
}
