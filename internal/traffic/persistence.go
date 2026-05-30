package traffic

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"hypnotz/internal/trafficmodel"
)

const recentAnomalyLimit = 100

// Persistor handles durable storage of speed histograms and anomalies in a
// local SQLite database. WAL mode is enabled for high-write concurrency.
type Persistor struct {
	db *sql.DB
}

// NewPersistor opens (or creates) the SQLite database at dbPath and ensures
// the schema is up to date.
func NewPersistor(dbPath string) (*Persistor, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", dbPath, err)
	}

	const schema = `
CREATE TABLE IF NOT EXISTS speed_samples (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    segment_id   INTEGER NOT NULL,
    hour_of_week INTEGER NOT NULL,
    speed_bin    INTEGER NOT NULL,
    count        INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE(segment_id, hour_of_week, speed_bin)
);
CREATE INDEX IF NOT EXISTS idx_segment ON speed_samples(segment_id);
CREATE INDEX IF NOT EXISTS idx_hour    ON speed_samples(hour_of_week);

CREATE TABLE IF NOT EXISTS anomalies (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    vehicle_id     TEXT    NOT NULL,
    segment_id     INTEGER NOT NULL,
    speed          REAL    NOT NULL,
    expected_speed REAL    NOT NULL,
    deviation      REAL    NOT NULL,
    risk_score     REAL    NOT NULL,
    timestamp      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_anomaly_time ON anomalies(timestamp);
`
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Persistor{db: db}, nil
}

// SaveHistogram persists all non-zero bins of histogram for segmentID.
func (p *Persistor) SaveHistogram(segmentID int64, histogram *trafficmodel.SpeedHistogram) error {
	bins := histogram.ExportBins()
	if len(bins) == 0 {
		return nil
	}

	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO speed_samples (segment_id, hour_of_week, speed_bin, count, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for binKey, count := range bins {
		if count == 0 {
			continue
		}
		hour := int(binKey) / trafficmodel.NumSpeedBins
		speedBin := int(binKey) % trafficmodel.NumSpeedBins
		if _, err = stmt.Exec(segmentID, hour, speedBin, count, now); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("insert bin (seg=%d h=%d b=%d): %w", segmentID, hour, speedBin, err)
		}
	}
	return tx.Commit()
}

// LoadHistogram reads the persisted bins for segmentID and returns a fully
// populated SpeedHistogram. Returns an empty histogram when no data exists.
func (p *Persistor) LoadHistogram(segmentID int64) (*trafficmodel.SpeedHistogram, error) {
	rows, err := p.db.Query(`
		SELECT hour_of_week, speed_bin, count
		FROM speed_samples
		WHERE segment_id = ?
	`, segmentID)
	if err != nil {
		return nil, fmt.Errorf("query histogram (seg=%d): %w", segmentID, err)
	}
	defer rows.Close()

	h := trafficmodel.NewSpeedHistogram()
	for rows.Next() {
		var hour, speedBin int
		var count int64
		if err := rows.Scan(&hour, &speedBin, &count); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		// Replay samples into the histogram via AddSample approximation.
		// We reconstruct by calling AddSample once per count unit at a
		// canonical timestamp for that hour-of-week.
		speedMS := float64(speedBin) * trafficmodel.SpeedBinSizeKmh / 3.6
		baseNS := int64(hour) * 3600 * int64(1e9)
		for i := int64(0); i < count; i++ {
			h.AddSample(baseNS, speedMS)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return h, nil
}

// SaveAnomaly records a single detected anomaly event.
func (p *Persistor) SaveAnomaly(vehicleID string, segmentID int64, speed, expected, deviation, risk float64, timestamp int64) error {
	_, err := p.db.Exec(`
		INSERT INTO anomalies (vehicle_id, segment_id, speed, expected_speed, deviation, risk_score, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, vehicleID, segmentID, speed, expected, deviation, risk, timestamp)
	if err != nil {
		return fmt.Errorf("save anomaly (vehicle=%s): %w", vehicleID, err)
	}
	return nil
}

// GetRecentAnomalies returns up to recentAnomalyLimit anomalies detected
// within the last minutes minutes, newest first.
func (p *Persistor) GetRecentAnomalies(minutes int) ([]AnomalyReport, error) {
	since := time.Now().Add(-time.Duration(minutes) * time.Minute).Unix()
	rows, err := p.db.Query(`
		SELECT vehicle_id, segment_id, speed, expected_speed, deviation, risk_score, timestamp
		FROM anomalies
		WHERE timestamp > ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, since, recentAnomalyLimit)
	if err != nil {
		return nil, fmt.Errorf("query recent anomalies: %w", err)
	}
	defer rows.Close()

	var out []AnomalyReport
	for rows.Next() {
		var a AnomalyReport
		if err := rows.Scan(&a.VehicleID, &a.SegmentID, &a.Speed, &a.ExpectedSpeed, &a.Deviation, &a.RiskScore, &a.Timestamp); err != nil {
			return nil, fmt.Errorf("scan anomaly row: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return out, nil
}

// Close releases the database connection.
func (p *Persistor) Close() error { return p.db.Close() }

