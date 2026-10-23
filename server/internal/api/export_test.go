package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

func lines(csv string) []string {
	return strings.Split(strings.TrimRight(csv, "\r\n"), "\r\n")
}

func flat(rate string) func(rates.Date) *rates.Money {
	m := rates.MustMoney(rate)
	return func(rates.Date) *rates.Money { return &m }
}

func ptrf(v float64) *float64 { return &v }

// These are the cases of HistoryExportTests in the C# test project, one for one.

func TestOneRowPerDayWithTheWholePcSensorsEachMeterAndTheCosts(t *testing.T) {
	csv := lines(dailyCSV([]exportDay{
		{Day: rates.MustDate("2026-10-18"), Wall: ptrf(0.01), Sensors: 0.004, Meters: []float64{0.005}},
		{Day: rates.MustDate("2026-10-19"), Wall: ptrf(0.02), Sensors: 0, Meters: []float64{0}},
	}, []string{"Monitor"}, flat("0.20")))
	want := []string{
		"date,whole_pc_kwh,sensors_kwh,Monitor (kwh),pc_cost,peripherals_cost",
		"2026-10-18,0.01,0.004,0.005,0.0020,0.0010",
		"2026-10-19,0.02,0,0,0.0040,0.0000",
	}
	if len(csv) != 3 || csv[0] != want[0] || csv[1] != want[1] || csv[2] != want[2] {
		t.Errorf("csv = %q", csv)
	}
}

func TestWithoutAWallPlugOrAPriceTheCellsAreBlankNotMadeUp(t *testing.T) {
	csv := lines(dailyCSV([]exportDay{{Day: rates.MustDate("2026-10-18"), Sensors: 0.004}}, nil, func(rates.Date) *rates.Money { return nil }))
	if csv[0] != "date,whole_pc_kwh,sensors_kwh,pc_cost,peripherals_cost" || csv[1] != "2026-10-18,,0.004,," {
		t.Errorf("csv = %q", csv)
	}
}

func TestALabelACellCouldRunAsAFormulaIsExportedAsTextAndCommasAreQuoted(t *testing.T) {
	header := lines(dailyCSV(nil, []string{`=HYPERLINK("http://x")`, "Desk, left", "-5 inches", "+cmd", "@sum"}, flat("0.20")))[0]
	for _, want := range []string{
		`"'=HYPERLINK(""http://x"") (kwh)"`, // quoted for its quotes, and an apostrophe in front
		`"Desk, left (kwh)"`,
		`'+cmd (kwh)`,
		`'@sum (kwh)`,
	} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q lacks %q", header, want)
		}
	}
	if strings.Contains(header, ",=HYPERLINK") {
		t.Errorf("a formula survived: %q", header)
	}
	// A plain negative number is a number, not a formula.
	if got := csvCell("-4.5"); got != "-4.5" {
		t.Errorf("csvCell(-4.5) = %q", got)
	}
	if got := csvCell("-HYPERLINK"); got != "'-HYPERLINK" {
		t.Errorf("csvCell(-HYPERLINK) = %q", got)
	}
}

func TestTheNumbersUseADotWhateverTheLanguage(t *testing.T) {
	if got := number(0.0042); got != "0.0042" {
		t.Errorf("number = %q", got)
	}
	if got := number(1234.5); got != "1234.5" {
		t.Errorf("number = %q, want no thousands separator", got)
	}
}

func TestTheComputerExportIsADailyCsvDownloadForThePeriod(t *testing.T) {
	clock := time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)
	postBody(t, srv.URL+"/api/v1/ingest/pc", contractExample(t, "ingest-pc.json"))
	id := detailOf(t, srv).Computer.ID

	resp, err := http.Get(srv.URL + "/api/v1/computers/" + id + "/export.csv?from=2026-10-21T00:00:00Z&to=2026-10-23T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), `attachment; filename="kvguzzler-`) {
		t.Fatalf("status %d, headers %v", resp.StatusCode, resp.Header)
	}
	var buf strings.Builder
	b := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(b)
		buf.Write(b[:n])
		if err != nil {
			break
		}
	}
	rows := lines(buf.String())
	if rows[0] != "date,whole_pc_kwh,sensors_kwh,pc_cost,peripherals_cost" || len(rows) != 3 {
		t.Fatalf("rows = %q", rows)
	}
	if !strings.HasPrefix(rows[1], "2026-10-21,,0,") || !strings.HasPrefix(rows[2], "2026-10-22,,") {
		t.Errorf("rows = %q: no wall plug means a blank whole-PC cell, and the sensors' energy lands on the 22nd", rows)
	}
	if r := get(t, srv.URL+"/api/v1/computers/"+id+"/export.csv?from=nope&to=2026-10-23T00:00:00Z", nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("bad period: %d", r.StatusCode)
	}
}
