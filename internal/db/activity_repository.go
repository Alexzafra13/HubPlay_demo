package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ActivityRepository serves the admin "playback activity" queries
// that power the System panel's sparkline (DailyWatchActivity) and
// Top-N rail (TopItems). Both derive their counts from `user_data`
// (the per-user last-played + position tracker) joined against
// `items`; HubPlay does not maintain a separate play-event log so
// `user_data.last_played_at` is the closest signal we have.
//
// Raw SQL — these are one-shot aggregates with dialect-divergent
// day-extraction grammar (SUBSTR vs TO_CHAR) and an episode→series
// rollup CTE that sqlc's positional-binding model handles
// awkwardly. Same rationale as `HomeRepository` (Pattern B).
//
// Vive en su propio fichero (no embebido en SystemHandler) para
// cerrar el olor K de la auditoría 2026-05-14: los handlers no
// deben recibir `*sql.DB` raw para hacer queries inline contra
// `user_data` / `items` — la inversión de dependencias correcta
// es que el handler consume una interfaz tipada del repo.
type ActivityRepository struct {
	db     *sql.DB
	driver string

	dailyWatchSQL string
	topItemsSQL   string
}

// DailyWatchBucket is one row of the daily watch-activity rollup.
// Date is YYYY-MM-DD in UTC; the handler / frontend localises display.
// WatchMinutes approximates engagement by integrating duration_ticks *
// progress over user_data rows updated within the bucket. SessionCount
// is distinct (user_id, item_id) pairs touched that day.
type DailyWatchBucket struct {
	Date         string
	WatchMinutes int
	SessionCount int
}

// TopItemRow is one row of the "most-watched in trailing window"
// admin rail. PlayCount = distinct users who touched a rollup item
// in the window (episode plays fold up to their series).
type TopItemRow struct {
	ID        string
	Type      string
	Title     string
	PlayCount int
}

// NewActivityRepository pre-rewrites the dialect-divergent queries
// once at construction. The day-extraction grammar diverges across
// dialects:
//
//   - SQLite stores last_played_at as text (modernc.org's time.Time
//     formatter trails ISO with " +0000 UTC", outside strftime
//     grammar) so we slice the first 10 chars with SUBSTR.
//   - Postgres has a real TIMESTAMPTZ column, so we TO_CHAR.
//
// Both produce a YYYY-MM-DD string the Go side uses as a map key.
//
// BOOLEAN predicates are written truthy (no `= 1`) so the same query
// runs against SQLite's 0/1 INTEGER and Postgres' BOOLEAN. `?`
// placeholders rewritten at construction time for the active driver.
func NewActivityRepository(driver string, database *sql.DB) *ActivityRepository {
	r := &ActivityRepository{db: database, driver: driver}

	dayExpr := "SUBSTR(ud.last_played_at, 1, 10)"
	if IsPostgres(driver) {
		dayExpr = "TO_CHAR(ud.last_played_at, 'YYYY-MM-DD')"
	}
	r.dailyWatchSQL = rewritePlaceholders(driver, fmt.Sprintf(`
		SELECT
			%s AS day,
			COALESCE(SUM(
				CASE
					WHEN i.duration_ticks > 0
						AND ud.position_ticks <= i.duration_ticks
						THEN ud.position_ticks / 600000000
					WHEN i.duration_ticks > 0
						THEN i.duration_ticks / 600000000
					ELSE 0
				END
			), 0) AS watch_minutes,
			COUNT(DISTINCT ud.user_id || ':' || ud.item_id) AS session_count
		FROM user_data ud
		JOIN items i ON i.id = ud.item_id
		WHERE ud.last_played_at IS NOT NULL
		  AND ud.last_played_at >= ?
		GROUP BY day
		ORDER BY day ASC`, dayExpr))

	// Same rollup CTE as HomeRepository.Trending but admin-scoped:
	// no library_access EXISTS guard. Counts each (user, rollup) once
	// so a single binge-watcher can't dominate the chart.
	r.topItemsSQL = rewritePlaceholders(driver, `
		WITH plays AS (
			SELECT
				ud.user_id,
				CASE
					WHEN i.type = 'episode' AND i.parent_id IS NOT NULL
						THEN COALESCE(
							(SELECT s.parent_id FROM items s WHERE s.id = i.parent_id),
							i.parent_id
						)
					ELSE i.id
				END AS rollup_id
			FROM user_data ud
			JOIN items i ON i.id = ud.item_id
			WHERE ud.last_played_at IS NOT NULL
			  AND ud.last_played_at >= ?
			  AND i.is_available
		)
		SELECT i.id, i.type, i.title, COUNT(DISTINCT p.user_id) AS plays
		FROM plays p
		JOIN items i ON i.id = p.rollup_id
		WHERE i.is_available
		GROUP BY i.id
		ORDER BY plays DESC, i.title ASC
		LIMIT ?`)

	return r
}

// DailyWatchActivity returns the per-day buckets observed in the
// window since `cutoff`. Days with no activity are NOT emitted —
// the caller backfills empty days with zero-value buckets so the
// sparkline renders a contiguous series.
//
// Cutoff is passed as time.Time so the driver formats it the same way
// last_played_at was written (UTC, no monotonic suffix because
// repos.UserData normalises). String pre-formatting rejected rows
// that the rest of the code happily matches.
func (r *ActivityRepository) DailyWatchActivity(ctx context.Context, cutoff time.Time) ([]DailyWatchBucket, error) {
	rows, err := r.db.QueryContext(ctx, r.dailyWatchSQL, cutoff)
	if err != nil {
		return nil, fmt.Errorf("daily watch activity: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := []DailyWatchBucket{}
	for rows.Next() {
		var b DailyWatchBucket
		// modernc.org/sqlite + the DATETIME affinity used by user_data
		// produce result sets where the strftime() output and SUM()
		// can come back as either string-encoded numbers or native
		// integers depending on the driver path. Scan into `any` and
		// coerce, identical pattern to how trending parses
		// MAX(last_played_at) elsewhere in the repo.
		var dateRaw, minutesRaw, sessionsRaw any
		if err := rows.Scan(&dateRaw, &minutesRaw, &sessionsRaw); err != nil {
			return nil, fmt.Errorf("scan daily watch row: %w", err)
		}
		switch v := dateRaw.(type) {
		case string:
			b.Date = v
		case []byte:
			b.Date = string(v)
		}
		switch v := minutesRaw.(type) {
		case int64:
			b.WatchMinutes = int(v)
		case float64:
			b.WatchMinutes = int(v)
		case []byte:
			if n, perr := atoiSafe(string(v)); perr == nil {
				b.WatchMinutes = n
			}
		case string:
			if n, perr := atoiSafe(v); perr == nil {
				b.WatchMinutes = n
			}
		}
		switch v := sessionsRaw.(type) {
		case int64:
			b.SessionCount = int(v)
		case float64:
			b.SessionCount = int(v)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// TopItems returns the top `limit` items watched across all users
// since `cutoff`. Episodes are rolled up to their series so the
// list reads like "Mr Robot · 12 plays" instead of polluting with
// individual episodes.
func (r *ActivityRepository) TopItems(ctx context.Context, cutoff time.Time, limit int) ([]TopItemRow, error) {
	rows, err := r.db.QueryContext(ctx, r.topItemsSQL, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("top items: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := make([]TopItemRow, 0, limit)
	for rows.Next() {
		var it TopItemRow
		if err := rows.Scan(&it.ID, &it.Type, &it.Title, &it.PlayCount); err != nil {
			return nil, fmt.Errorf("scan top item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// atoiSafe is a small unsigned-int parser, shared with handlers
// when they have to coerce SUM() output that the driver returns as
// a string. Avoids dragging strconv in for one use.
func atoiSafe(s string) (int, error) {
	n := 0
	negative := false
	for i, c := range s {
		if i == 0 && c == '-' {
			negative = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0, fmt.Errorf("overflow: %q", s)
		}
	}
	if negative {
		n = -n
	}
	return n, nil
}
