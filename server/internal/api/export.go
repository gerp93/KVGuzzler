package api

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// exportDay is one local day of a computer's figures, as the CSV writes them.
type exportDay struct {
	Day     rates.Date
	Wall    *float64 // the whole PC at its wall plug; nil when it has none
	Sensors float64
	Meters  []float64 // one per meter, in the order of the labels
	// PcKg and MetersKg are the day's estimated CO2 for the PC and for the meters, in kilograms; nil when the carbon footprint has nothing.
	PcKg, MetersKg *float64
}

// dailyCSV writes one row per day: the whole PC at the wall (blank without a wall plug), what the sensors measured, a column for each
// meter, and the day's cost of the PC and of the meters at the price on that day (blank when no price is known). Numbers use a dot and no
// thousands separator, so any spreadsheet reads them. A port of KVGuzzler.Core.History.HistoryExport.DailyCsv (carbon columns not yet).
func dailyCSV(days []exportDay, meterLabels []string, rateAt func(rates.Date) *rates.Money) string {
	var b strings.Builder
	header := []string{"date", "whole_pc_kwh", "sensors_kwh"}
	for _, m := range meterLabels {
		header = append(header, m+" (kwh)")
	}
	header = append(header, "pc_cost", "peripherals_cost")
	withCarbon := len(days) > 0 && days[0].PcKg != nil
	if withCarbon {
		header = append(header, "pc_co2_kg", "peripherals_co2_kg")
	}
	b.WriteString(csvLine(header))

	for _, d := range days {
		row := []string{d.Day.String(), "", number(d.Sensors)}
		if d.Wall != nil {
			row[1] = number(*d.Wall)
		}
		meters := 0.0
		for _, k := range d.Meters {
			row = append(row, number(k))
			meters += k
		}
		cost := func(kwh float64) string {
			if rate := rateAt(d.Day); rate != nil {
				return rates.MoneyFromFloat(kwh).Mul(*rate).Fixed(4)
			}
			return ""
		}
		pc := d.Sensors
		if d.Wall != nil {
			pc = *d.Wall
		}
		row = append(row, cost(pc), cost(meters))
		if withCarbon {
			pcKg, metersKg := "", "0"
			if d.PcKg != nil {
				pcKg = number(*d.PcKg)
			}
			if len(d.Meters) > 0 && d.MetersKg != nil {
				metersKg = number(*d.MetersKg)
			}
			row = append(row, pcKg, metersKg)
		}
		b.WriteString(csvLine(row))
	}
	return b.String()
}

func csvLine(cells []string) string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = csvCell(c)
	}
	return strings.Join(out, ",") + "\r\n"
}

// number is a value to at most six decimals with no trailing zeros ("0.004", "0").
func number(v float64) string { return strconv.FormatFloat(round6(v), 'f', -1, 64) }

func round6(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 6, 64), 64)
	return f
}

// csvCell quotes a cell that has a comma, quote or line break. A name a spreadsheet would run as a formula (starting with = + @, or a
// - that is not part of a number) gets a leading apostrophe, so a label can never do anything but show up as text.
func csvCell(text string) string {
	if text != "" {
		first := text[0]
		_, notANumber := strconv.ParseFloat(text, 64)
		if first == '=' || first == '+' || first == '@' || (first == '-' && len(text) > 1 && notANumber != nil) {
			text = "'" + text
		}
	}
	if strings.ContainsAny(text, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(text, "\"", "\"\"") + "\""
	}
	return text
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// dayBounds is the part of a local day inside [from, to).
func (s *Server) dayBounds(day rates.Date, from, to time.Time) (time.Time, time.Time) {
	f, t := s.scheduleDayStart(day), s.scheduleDayStart(day.AddDays(1))
	if f.Before(from) {
		f = from
	}
	if t.After(to) {
		t = to
	}
	return f, t
}

func (s *Server) scheduleDayStart(day rates.Date) time.Time {
	return time.Date(day.Year, day.Month, day.Day, 0, 0, 0, 0, s.Loc)
}

// computerExport is a computer's daily energy and cost over a period as a CSV download: one row per local day, the whole PC at its wall
// plug, what its sensors measured, and each plug next to it.
func (s *Server) computerExport(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	comp, err := s.Store.Computer(r.Context(), r.PathValue("computerId"))
	if err != nil {
		computerError(w, err)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	_, byID, err := s.enabledChannels(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}

	perDay := func(channelID string) (map[rates.Date]float64, error) {
		points, err := s.Store.Series(r.Context(), channelID, from, to, model.BucketDay, s.Loc)
		out := map[rates.Date]float64{}
		for _, p := range points {
			out[rates.DateOf(p.T, s.Loc)] += p.KWh
		}
		return out, err
	}

	var wall map[rates.Date]float64
	if ch, ok := byID[derefString(comp.WallChannelID)]; ok && ch.Enabled {
		if wall, err = perDay(ch.ID); err != nil {
			serverError(w, err)
			return
		}
	}
	var labels []string
	var meters []map[rates.Date]float64
	for _, id := range comp.PeripheralChannelIDs {
		ch, ok := byID[id]
		if !ok || !ch.Enabled {
			continue
		}
		m, err := perDay(id)
		if err != nil {
			serverError(w, err)
			return
		}
		labels, meters = append(labels, ch.Label), append(meters, m)
	}
	sensors := map[rates.Date]float64{}
	if comp.SourceID != nil {
		series, err := s.Store.PcSeries(r.Context(), *comp.SourceID, from, to, model.BucketDay, s.Loc)
		if err != nil {
			serverError(w, err)
			return
		}
		if series != nil {
			for _, l := range series.Lines {
				if l.Scope != "component" {
					continue
				}
				for _, p := range l.Points {
					sensors[rates.DateOf(p.T, s.Loc)] += p.KWh
				}
			}
		}
	}

	// The carbon footprint, a day at a time, once any grid intensity has been saved.
	ever, haveCarbon, err := s.Store.AverageCarbonEver(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var known map[int64]float64
	var wallID string
	var meterIDs []string
	if haveCarbon {
		if known, err = s.Store.QueryCarbon(r.Context(), from, to); err != nil {
			serverError(w, err)
			return
		}
		if ch, ok := byID[derefString(comp.WallChannelID)]; ok && ch.Enabled {
			wallID = ch.ID
		}
		for _, id := range comp.PeripheralChannelIDs {
			if ch, ok := byID[id]; ok && ch.Enabled {
				meterIDs = append(meterIDs, id)
			}
		}
	}
	dayKg := func(day rates.Date, byHour func(f, t time.Time) (map[int64]float64, error)) (*float64, error) {
		f, t := s.dayBounds(day, from, to)
		hours, err := byHour(f, t)
		if err != nil {
			return nil, err
		}
		kg := carbonResult(known, ever, hours).Kg
		return &kg, nil
	}

	var days []exportDay
	last := rates.DateOf(to.Add(-time.Nanosecond), s.Loc)
	for day := rates.DateOf(from, s.Loc); !day.After(last); day = day.AddDays(1) {
		d := exportDay{Day: day, Sensors: sensors[day]}
		if haveCarbon {
			var err error
			if d.PcKg, err = dayKg(day, func(f, t time.Time) (map[int64]float64, error) {
				switch {
				case wallID != "":
					return s.Store.HourlyKWh(r.Context(), []string{wallID}, f, t)
				case comp.SourceID != nil:
					return s.Store.PcHourlyKWh(r.Context(), *comp.SourceID, f, t)
				}
				return map[int64]float64{}, nil
			}); err != nil {
				serverError(w, err)
				return
			}
			if d.MetersKg, err = dayKg(day, func(f, t time.Time) (map[int64]float64, error) {
				return s.Store.HourlyKWh(r.Context(), meterIDs, f, t)
			}); err != nil {
				serverError(w, err)
				return
			}
		}
		if wall != nil {
			v := wall[day]
			d.Wall = &v
		}
		for _, m := range meters {
			d.Meters = append(d.Meters, m[day])
		}
		days = append(days, d)
	}

	name := unsafeFileChars.ReplaceAllString(comp.Name, "-")
	file := filepath.Base("kvguzzler-" + name + "-daily-" + rates.DateOf(from, s.Loc).String() + "-to-" + last.String() + ".csv")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+file+`"`)
	_, _ = w.Write([]byte(dailyCSV(days, labels, schedule.RateAt)))
}
