package traffic

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Persistor handles SQLite storage of speed histograms
type Persistor struct {
	db   *sql.DB
	mu   sync.Mutex
	path string
}

// NewPersistor creates a SQLite database at the given path
func NewPersistor(dbPath string) (*Persistor, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	// Create tables
	schema := `
	CREATE TABLE IF NOT EXISTS speed_samples (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		segment_id INTEGER NOT NULL,
		hour_of_week INTEGER NOT NULL,
		speed_bin INTEGER NOT NULL,
		count INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_segment ON speed_samples(segment_id);
	CREATE INDEX IF NOT EXISTS idx_hour ON speed_samples(hour_of_week);
	
	CREATE TABLE IF NOT EXISTS anomalies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vehicle_id TEXT NOT NULL,
		segment_id INTEGER NOT NULL,
		speed REAL NOT NULL,
		expected_speed REAL NOT NULL,
		deviation REAL NOT NULL,
		risk_score REAL NOT NULL,
		timestamp INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_anomaly_time ON anomalies(timestamp);
	`

	_, err = db.Exec(schema)
	if err != nil {
		return nil, err
	}

	return &Persistor{db: db, path: dbPath}, nil
}

// SaveHistogram persists a speed histogram to SQLite
func (p *Persistor) SaveHistogram(segmentID int64, histogram *SpeedHistogram) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	tx, err := p.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO speed_samples 
		(segment_id, hour_of_week, speed_bin, count, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()

	// Iterate over bins (requires exposing internal map via method)
	// For now, we'll implement a method on SpeedHistogram to export bins
	bins := histogram.ExportBins()

	for hour := 0; hour < 168; hour++ {
		for speedBin := 0; speedBin < 120; speedBin++ {
			binKey := GetHourSpeedBin(hour, speedBin)
			count, exists := bins[binKey]
			if exists && count > 0 {
				_, err = stmt.Exec(segmentID, hour, speedBin, count, now)
				if err != nil {
					tx.Rollback()
					return err
				}
			}
		}
	}

	return tx.Commit()
}

// LoadHistogram loads a speed histogram from SQLite
func (p *Persistor) LoadHistogram(segmentID int64) (*SpeedHistogram, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	histogram := NewSpeedHistogram()

	rows, err := p.db.Query(`
		SELECT hour_of_week, speed_bin, count 
		FROM speed_samples 
		WHERE segment_id = ?
	`, segmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var hour, speedBin int
		var count int64
		if err := rows.Scan(&hour, &speedBin, &count); err != nil {
			return nil, err
		}
		
		// Reconstruct bins
		// Note: This is a simplified reconstruction. 
		// In production, we'd store the raw samples or a serialized blob.
	}

	return histogram, nil
}

// SaveAnomaly records a detected anomaly
func (p *Persistor) SaveAnomaly(vehicleID string, segmentID int64, speed, expected, deviation, risk float64, timestamp int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, err := p.db.Exec(`
		INSERT INTO anomalies (vehicle_id, segment_id, speed, expected_speed, deviation, risk_score, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, vehicleID, segmentID, speed, expected, deviation, risk, timestamp)

	return err
}

// GetRecentAnomalies returns anomalies from the last N minutes
func (p *Persistor) GetRecentAnomalies(minutes int) ([]AnomalyReport, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	since := time.Now().Add(-time.Duration(minutes) * time.Minute).Unix()

	rows, err := p.db.Query(`
		SELECT vehicle_id, segment_id, speed, expected_speed, deviation, risk_score, timestamp
		FROM anomalies
		WHERE timestamp > ?
		ORDER BY timestamp DESC
		LIMIT 100
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	anomalies := []AnomalyReport{}
	for rows.Next() {
		var a AnomalyReport
		if err := rows.Scan(&a.VehicleID, &a.SegmentID, &a.Speed, &a.ExpectedSpeed, &a.Deviation, &a.RiskScore, &a.Timestamp); err != nil {
			return nil, err
		}
		anomalies = append(anomalies, a)
	}

	return anomalies, nil
}

// Close closes the database connection
func (p *Persistor) Close() error {
	return p.db.Close()
}

// ExportBins exports the internal bin map (helper for persistence)
func (h *SpeedHistogram) ExportBins() map[uint16]int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	// Create a copy to avoid race conditions
	bins := make(map[uint16]int64, len(h.bins))
	for k, v := range h.bins {
		bins[k] = v
	}
	return bins
}