package store

import (
	"context"
	"database/sql"
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
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(in.Minutes), nil
}

// PcSourceSummary is what one PC collector reported over [from, to): its sensors, its wall figure and peripherals, the
// per-program estimates, and the stretches it was not sampling (clipped to the period, with a plug counter's energy shared
// evenly over the gap so any period gets its share). It is nil when that collector has never reported.
func (s *Store) PcSourceSummary(ctx context.Context, sourceID string, from, to time.Time) (*model.PcSource, error) {
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
SELECT name, scope, kind, SUM(avg_w * seconds), MAX(max_w), SUM(seconds)
FROM pc_minutes WHERE source_id = ? AND minute >= ? AND minute < ?
GROUP BY scope, name, kind ORDER BY SUM(avg_w * seconds) DESC`, id, f, t)
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
