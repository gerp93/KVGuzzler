package rates

import (
	"encoding/json"
	"fmt"
	"time"
)

// Date is a calendar day with no time or zone. Bill dates are local calendar days.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

const dateLayout = "2006-01-02"

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("%q is not a date: use YYYY-MM-DD", s)
	}
	return DateFrom(t), nil
}

func MustDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// DateFrom is the calendar day of t as t's own clock reads it.
func DateFrom(t time.Time) Date { return Date{t.Year(), t.Month(), t.Day()} }

// DateOf is the calendar day of an instant in loc.
func DateOf(t time.Time, loc *time.Location) Date { return DateFrom(t.In(loc)) }

func (d Date) utc() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

func (d Date) String() string { return d.utc().Format(dateLayout) }

func (d Date) AddDays(n int) Date { return DateFrom(d.utc().AddDate(0, 0, n)) }

func (d Date) Before(o Date) bool { return d.utc().Before(o.utc()) }
func (d Date) After(o Date) bool  { return d.utc().After(o.utc()) }

// DaysSince is how many days d is after o (negative when before).
func (d Date) DaysSince(o Date) int { return int(d.utc().Sub(o.utc()).Hours() / 24) }

func (d Date) Weekday() time.Weekday { return d.utc().Weekday() }

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("a date is a string like \"2026-09-21\"")
	}
	v, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}
