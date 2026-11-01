package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/pcrelay"
)

const pcSchema = `
CREATE TABLE IF NOT EXISTS pc_sources (
  source_id TEXT PRIMARY KEY,
  name      TEXT NOT NULL DEFAULT '',
  last_seen INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS pc_minutes (
  source_id TEXT NOT NULL,
  minute    INTEGER NOT NULL,
  name      TEXT NOT NULL,
  scope     TEXT NOT NULL,
  kind      TEXT NOT NULL,
  avg_w     REAL NOT NULL,
  max_w     REAL NOT NULL,
  seconds   REAL NOT NULL,
  PRIMARY KEY (source_id, minute, scope, name)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS pc_programs (
  source_id TEXT NOT NULL,
  minute    INTEGER NOT NULL,
  name      TEXT NOT NULL COLLATE NOCASE,
  cpu_kwh   REAL NOT NULL,
  gpu_kwh   REAL NOT NULL,
  active_s  REAL NOT NULL,
  PRIMARY KEY (source_id, minute, name)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS pc_peaks (
  source_id TEXT NOT NULL,
  minute    INTEGER NOT NULL,
  watts     REAL NOT NULL,
  PRIMARY KEY (source_id, minute)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS pc_live (
  source_id TEXT PRIMARY KEY,
  received  INTEGER NOT NULL,
  body      TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS pc_runs (
  source_id TEXT NOT NULL,
  name      TEXT NOT NULL COLLATE NOCASE,
  start_ts  INTEGER NOT NULL,
  end_ts    INTEGER NOT NULL,
  cpu_kwh   REAL NOT NULL,
  gpu_kwh   REAL NOT NULL,
  active_s  REAL NOT NULL,
  still     INTEGER NOT NULL,
  PRIMARY KEY (source_id, name, start_ts)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS pc_gaps (
  source_id TEXT NOT NULL,
  from_ts   INTEGER NOT NULL,
  to_ts     INTEGER NOT NULL,
  reason    TEXT NOT NULL,
  plug_kwh  REAL,
  PRIMARY KEY (source_id, from_ts)
) WITHOUT ROWID;`

// IngestPC stores what the PC collector pushed. Each minute, program, peak and gap is keyed by what it is, so sending
// the same thing again replaces it: the PC can resend its whole backlog after a failure. It returns how many minutes
// were stored. The body must already have passed pcrelay.Ingest.Validate.
func (s *Store) IngestPC(ctx context.Context, in pcrelay.Ingest, now time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO pc_sources(source_id, name, last_seen) VALUES (?, ?, ?)
ON CONFLICT(source_id) DO UPDATE SET name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE name END, last_seen = excluded.last_seen`,
		in.SourceID, in.SourceName, now.Unix()); err != nil {
		return 0, err
	}
	if err := ensureComputerForSource(ctx, tx, in.SourceID, in.SourceName); err != nil {
		return 0, err
	}
	for _, m := range in.Minutes {
		at := m.Minute.Unix()
		for _, r := range m.Readings {
			kind := r.Kind
			if kind == "" {
				kind = "other"
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO pc_minutes(source_id, minute, name, scope, kind, avg_w, max_w, seconds) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				in.SourceID, at, r.Name, r.Scope, kind, r.AvgWatts, r.MaxWatts, float64(r.Samples)); err != nil {
				return 0, err
			}
		}
		for _, p := range m.Programs {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO pc_programs(source_id, minute, name, cpu_kwh, gpu_kwh, active_s) VALUES (?, ?, ?, ?, ?, ?)`,
				in.SourceID, at, p.Name, p.CPUKwh, p.GPUKwh, p.ActiveSeconds); err != nil {
				return 0, err
			}
		}
		if m.TotalPeakWatts != nil {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO pc_peaks(source_id, minute, watts) VALUES (?, ?, ?)`, in.SourceID, at, *m.TotalPeakWatts); err != nil {
				return 0, err
			}
		}
	}
	for _, g := range in.Gaps {
		var plug any
		if g.PlugKwh != nil {
			plug = *g.PlugKwh
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO pc_gaps(source_id, from_ts, to_ts, reason, plug_kwh) VALUES (?, ?, ?, ?, ?)`,
			in.SourceID, g.From.Unix(), g.To.Unix(), g.Reason, plug); err != nil {
			return 0, err
		}
	}
	if in.Live != nil {
		body, err := json.Marshal(in.Live)
		if err != nil {
			return 0, err
		}
		// Only the newest look is kept: a late or repeated one does not replace a newer one.
		if _, err := tx.ExecContext(ctx, `
INSERT INTO pc_live(source_id, received, body) VALUES (?, ?, ?)
ON CONFLICT(source_id) DO UPDATE SET received = excluded.received, body = excluded.body
WHERE json_extract(excluded.body, '$.at') >= json_extract(pc_live.body, '$.at')`, in.SourceID, now.Unix(), string(body)); err != nil {
			return 0, err
		}
	}
	for _, r := range in.Runs {
		still := 0
		if r.StillRunning {
			still = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO pc_runs(source_id, name, start_ts, end_ts, cpu_kwh, gpu_kwh, active_s, still) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			in.SourceID, r.Name, r.Start.Unix(), r.End.Unix(), r.CPUKwh, r.GPUKwh, r.ActiveSeconds, still); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(in.Minutes), nil
}

// A reading is one line however its kind was recorded over time: history saved before the PC knew a sensor's kind says "other", later
// minutes say cpu or gpu, and the summary takes the specific kind when any minute has one.
//
// PcSourceSummary is what one PC collector reported over [from, to): its sensors, its wall figure and peripherals, the
// per-program estimates, and the stretches it was not sampling (clipped to the period, with a plug counter's energy shared
// evenly over the gap so any period gets its share). It is nil when that collector has never reported.
func (s *Store) PcSourceSummary(ctx context.Context, sourceID string, from, to time.Time) (*model.PcSource, error) {
	from = s.clip(from)
	sources, err := s.pcSources(ctx)
	if err != nil {
		return nil, err
	}
	for i := range sources {
		src := &sources[i]
		if src.SourceID != sourceID {
			continue
		}
		f, t := from.Unix(), to.Unix()
		if src.Components, err = s.pcComponents(ctx, src.SourceID, f, t); err != nil {
			return nil, err
		}
		if src.Programs, err = s.pcPrograms(ctx, src.SourceID, f, t); err != nil {
			return nil, err
		}
		if src.TotalPeakWatts, err = s.pcPeak(ctx, src.SourceID, f, t); err != nil {
			return nil, err
		}
		if src.Gaps, err = s.pcGaps(ctx, src.SourceID, f, t); err != nil {
			return nil, err
		}
		if src.Live, err = s.PcLive(ctx, src.SourceID); err != nil {
			return nil, err
		}
		if src.Runs, err = s.pcRuns(ctx, src.SourceID, f, t); err != nil {
			return nil, err
		}
		return src, nil
	}
	return nil, nil
}

func (s *Store) pcSources(ctx context.Context) ([]model.PcSource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, name, last_seen FROM pc_sources ORDER BY name, source_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []model.PcSource{}
	for rows.Next() {
		var src model.PcSource
		var seen int64
		if err := rows.Scan(&src.SourceID, &src.Name, &seen); err != nil {
			return nil, err
		}
		t := time.Unix(seen, 0).UTC()
		src.LastSeen = &t
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *Store) pcComponents(ctx context.Context, id string, f, t int64) ([]model.PcComponent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, scope,
       COALESCE(NULLIF(MAX(CASE WHEN kind <> 'other' THEN kind ELSE '' END), ''), 'other'),
       SUM(avg_w * seconds), MAX(max_w), SUM(seconds)
FROM pc_minutes WHERE source_id = ? AND minute >= ? AND minute < ?
GROUP BY scope, name ORDER BY SUM(avg_w * seconds) DESC`, id, f, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PcComponent{}
	for rows.Next() {
		var c model.PcComponent
		var ws, peak, secs float64
		if err := rows.Scan(&c.Name, &c.Scope, &c.Kind, &ws, &peak, &secs); err != nil {
			return nil, err
		}
		c.KWh = ws / joulesPerKWh
		c.MonitoredHours = secs / 3600
		if secs > 0 {
			avg := ws / secs
			c.AvgWatts, c.PeakWatts = &avg, &peak
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) pcPrograms(ctx context.Context, id string, f, t int64) ([]model.PcProgramTotal, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, SUM(cpu_kwh), SUM(gpu_kwh), SUM(active_s)
FROM pc_programs WHERE source_id = ? AND minute >= ? AND minute < ?
GROUP BY name ORDER BY SUM(cpu_kwh) + SUM(gpu_kwh) DESC LIMIT 100`, id, f, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PcProgramTotal{}
	for rows.Next() {
		var p model.PcProgramTotal
		if err := rows.Scan(&p.Name, &p.CPUKwh, &p.GPUKwh, &p.ActiveSeconds); err != nil {
			return nil, err
		}
		p.KWh = p.CPUKwh + p.GPUKwh
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) pcPeak(ctx context.Context, id string, f, t int64) (*float64, error) {
	var peak sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(watts) FROM pc_peaks WHERE source_id = ? AND minute >= ? AND minute < ?`, id, f, t).Scan(&peak); err != nil {
		return nil, err
	}
	if !peak.Valid {
		return nil, nil
	}
	v := peak.Float64
	return &v, nil
}

func (s *Store) pcGaps(ctx context.Context, id string, f, t int64) ([]model.PcGap, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT from_ts, to_ts, reason, plug_kwh FROM pc_gaps
WHERE source_id = ? AND to_ts > ? AND from_ts < ? ORDER BY from_ts`, id, f, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PcGap{}
	for rows.Next() {
		var gf, gt int64
		var reason string
		var plug sql.NullFloat64
		if err := rows.Scan(&gf, &gt, &reason, &plug); err != nil {
			return nil, err
		}
		cf, ct := max(gf, f), min(gt, t)
		g := model.PcGap{From: time.Unix(cf, 0).UTC(), To: time.Unix(ct, 0).UTC(), Reason: reason}
		if plug.Valid && gt > gf {
			share := plug.Float64 * float64(ct-cf) / float64(gt-gf)
			g.PlugKwh = &share
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// PcLive is the newest look the PC sent, or nil. ReceivedAt is the server's own clock, which the API turns into an age.
func (s *Store) PcLive(ctx context.Context, id string) (*model.PcLive, error) {
	var received int64
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT received, body FROM pc_live WHERE source_id = ?`, id).Scan(&received, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l model.PcLive
	if err := json.Unmarshal([]byte(body), &l); err != nil {
		return nil, err
	}
	l.ReceivedAt = time.Unix(received, 0).UTC()
	if l.Readings == nil {
		l.Readings = []model.PcLiveReading{}
	}
	if l.Programs == nil {
		l.Programs = []model.PcLiveProgram{}
	}
	if l.Notes == nil {
		l.Notes = []string{}
	}
	return &l, nil
}

// pcRuns are the program runs that overlapped [f, t): the ones still going first, then the newest.
func (s *Store) pcRuns(ctx context.Context, id string, f, t int64) ([]model.PcRun, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, start_ts, end_ts, cpu_kwh, gpu_kwh, active_s, still FROM pc_runs
WHERE source_id = ? AND end_ts >= ? AND start_ts < ? ORDER BY still DESC, end_ts DESC LIMIT 50`, id, f, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PcRun{}
	for rows.Next() {
		var r model.PcRun
		var start, end int64
		var still int
		if err := rows.Scan(&r.Name, &start, &end, &r.CPUKwh, &r.GPUKwh, &r.ActiveSeconds, &still); err != nil {
			return nil, err
		}
		r.Start, r.End, r.StillRunning = time.Unix(start, 0).UTC(), time.Unix(end, 0).UTC(), still == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// PcSeries is a PC collector's sensors, wall plug and meters over [from, to), bucketed like Series, with the gaps inside the period.
// The readings that are parts of a sensor (scope breakdown) are left out: they are not separate lines. Nil when it never reported.
func (s *Store) PcSeries(ctx context.Context, sourceID string, from, to time.Time, bucket model.Bucket, loc *time.Location) (*model.PcSeries, error) {
	from = s.clip(from)
	var known int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pc_sources WHERE source_id = ?`, sourceID).Scan(&known); err != nil {
		return nil, err
	}
	if known == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT scope, name, kind, minute, avg_w * seconds, max_w, seconds FROM pc_minutes
WHERE source_id = ? AND minute >= ? AND minute < ? AND scope IN ('component', 'wall', 'peripheral') ORDER BY minute`,
		sourceID, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type key struct{ scope, name string }
	type line struct {
		kind    string
		buckets map[int64]*agg
	}
	lines := map[key]*line{}
	for rows.Next() {
		var k key
		var kind string
		var minute int64
		var a agg
		if err := rows.Scan(&k.scope, &k.name, &kind, &minute, &a.ws, &a.max, &a.secs); err != nil {
			return nil, err
		}
		l := lines[k]
		if l == nil {
			l = &line{kind: "other", buckets: map[int64]*agg{}}
			lines[k] = l
		}
		if kind != "other" {
			l.kind = kind
		}
		at := bucketStart(time.Unix(minute, 0), bucket, loc).Unix()
		if b := l.buckets[at]; b != nil {
			b.ws += a.ws
			b.secs += a.secs
			b.max = maxF(b.max, a.max)
		} else {
			l.buckets[at] = &a
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := &model.PcSeries{Bucket: bucket, Lines: []model.PcSeriesLine{}}
	for k, l := range lines {
		times := make([]int64, 0, len(l.buckets))
		for t := range l.buckets {
			times = append(times, t)
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		pl := model.PcSeriesLine{Name: k.name, Scope: k.scope, Kind: l.kind, Points: make([]model.Point, 0, len(times))}
		for _, t := range times {
			b := l.buckets[t]
			if b.secs <= 0 {
				continue
			}
			pl.Points = append(pl.Points, model.Point{T: time.Unix(t, 0).UTC(), AvgWatts: b.ws / b.secs, MaxWatts: b.max, KWh: b.ws / joulesPerKWh})
		}
		out.Lines = append(out.Lines, pl)
	}
	// The wall line first, then the sensors, then the meters; within each, by name, so the order never shuffles between reads.
	rank := map[string]int{"wall": 0, "component": 1, "peripheral": 2}
	sort.Slice(out.Lines, func(i, j int) bool {
		a, b := out.Lines[i], out.Lines[j]
		if rank[a.Scope] != rank[b.Scope] {
			return rank[a.Scope] < rank[b.Scope]
		}
		return a.Name < b.Name
	})

	if out.Gaps, err = s.pcGaps(ctx, sourceID, from.Unix(), to.Unix()); err != nil {
		return nil, err
	}
	return out, nil
}

// PcSensorsKwh is the energy the PC's sensors (the separate pieces of hardware, not the parts inside one) measured over [from, to).
func (s *Store) PcSensorsKwh(ctx context.Context, sourceID string, from, to time.Time) (float64, error) {
	from = s.clip(from)
	var ws sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `SELECT SUM(avg_w * seconds) FROM pc_minutes WHERE source_id = ? AND scope = 'component' AND minute >= ? AND minute < ?`,
		sourceID, from.Unix(), to.Unix()).Scan(&ws)
	if err != nil {
		return 0, err
	}
	return ws.Float64 / joulesPerKWh, nil
}

// PcGaps are the stretches the PC was not recording inside [from, to), clipped to it, with a plug counter's energy shared evenly.
func (s *Store) PcGaps(ctx context.Context, sourceID string, from, to time.Time) ([]model.PcGap, error) {
	from = s.clip(from)
	return s.pcGaps(ctx, sourceID, from.Unix(), to.Unix())
}

// PcFirstSensorMinute is the first minute the PC's sensors have anything recorded, or false when they have none.
func (s *Store) PcFirstSensorMinute(ctx context.Context, sourceID string) (time.Time, bool, error) {
	var first sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(minute) FROM pc_minutes WHERE source_id = ? AND scope = 'component' AND minute >= ?`, sourceID, s.floorUnix()).Scan(&first); err != nil {
		return time.Time{}, false, err
	}
	if !first.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(first.Int64, 0).UTC(), true, nil
}
