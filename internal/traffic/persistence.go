package traffic

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"hypnotz/internal/spectral"
	"hypnotz/internal/trafficmodel"
)

const recentAnomalyLimit = 100

// Persistor handles durable storage of speed histograms, anomaly events, and
// spectral signatures in a local SQLite database. WAL mode is enabled for
// high-write concurrency.
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

-- spectral_signatures stores the most recent frequency-domain fingerprint
-- for each vehicle. One row per vehicle; INSERT OR REPLACE keeps it current.
-- EntropyHistory is intentionally excluded — it is a rolling window that
-- reconstructs quickly from live samples and is not worth persisting.
CREATE TABLE IF NOT EXISTS spectral_signatures (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    vehicle_id       TEXT    NOT NULL,
    anomaly_score    REAL    NOT NULL,
    dominant_freq    REAL    NOT NULL,
    spectral_entropy REAL    NOT NULL,
    high_band_energy REAL    NOT NULL,
    low_band_energy  REAL    NOT NULL,
    mid_band_energy  REAL    NOT NULL,
    coherence_score  REAL    NOT NULL,
    route_signature  BLOB,
    updated_at       INTEGER NOT NULL,
    UNIQUE(vehicle_id)
);
CREATE INDEX IF NOT EXISTS idx_spectral_anomaly ON spectral_signatures(anomaly_score DESC);
CREATE INDEX IF NOT EXISTS idx_spectral_updated ON spectral_signatures(updated_at DESC);
`
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Persistor{db: db}, nil
}

// ─── Speed histogram ───────────────────────────────────────────────────────────

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

// LoadHistogram reads persisted bins for segmentID and returns a populated
// SpeedHistogram. Returns an empty histogram when no data exists.
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
		speedMS := float64(speedBin) * trafficmodel.SpeedBinSizeKmh / 3.6
		baseNS := int64(hour) * 3600 * int64(1e9)
		for i := int64(0); i < count; i++ {
			h.AddSample(baseNS, speedMS)
		}
	}
	return h, rows.Err()
}

// ─── Anomalies ────────────────────────────────────────────────────────────────

// SaveAnomaly records a single detected anomaly event.
func (p *Persistor) SaveAnomaly(vehicleID string, segmentID int64, speed, expected, deviation, risk float64, timestamp int64) error {
	_, err := p.db.Exec(`
		INSERT INTO anomalies
		    (vehicle_id, segment_id, speed, expected_speed, deviation, risk_score, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, vehicleID, segmentID, speed, expected, deviation, risk, timestamp)
	if err != nil {
		return fmt.Errorf("save anomaly (vehicle=%s): %w", vehicleID, err)
	}
	return nil
}

// GetRecentAnomalies returns up to recentAnomalyLimit anomaly events from the
// last minutes minutes, newest first.
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
		if err := rows.Scan(
			&a.VehicleID, &a.SegmentID, &a.Speed,
			&a.ExpectedSpeed, &a.Deviation, &a.RiskScore, &a.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan anomaly row: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ─── Spectral signatures ──────────────────────────────────────────────────────

// SaveSpectralSignature upserts the current SpectralProfile for vehicleID.
// The RouteSignature (FFT magnitude coefficients) is stored as a
// little-endian float32 blob. EntropyHistory is not persisted — it
// reconstructs quickly from live samples.
func (p *Persistor) SaveSpectralSignature(vehicleID string, profile *spectral.SpectralProfile) error {
	if profile == nil {
		return nil
	}
	_, err := p.db.Exec(`
		INSERT OR REPLACE INTO spectral_signatures
		    (vehicle_id, anomaly_score, dominant_freq, spectral_entropy,
		     high_band_energy, low_band_energy, mid_band_energy,
		     coherence_score, route_signature, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		vehicleID,
		float64(profile.AnomalyScore),
		profile.Features.DominantFrequency,
		profile.Features.SpectralEntropy,
		profile.Features.HighBandEnergy,
		profile.Features.LowBandEnergy,
		profile.Features.MidBandEnergy,
		profile.Features.CoherenceScore,
		encodeFloat32Slice(profile.RouteSignature),
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("save spectral signature (vehicle=%s): %w", vehicleID, err)
	}
	return nil
}

// LoadSpectralSignature retrieves the persisted SpectralProfile for vehicleID.
// Returns (nil, nil) when no signature exists for the vehicle.
// EntropyHistory is not restored — it rebuilds from live observations.
func (p *Persistor) LoadSpectralSignature(vehicleID string) (*spectral.SpectralProfile, error) {
	row := p.db.QueryRow(`
		SELECT anomaly_score, dominant_freq, spectral_entropy,
		       high_band_energy, low_band_energy, mid_band_energy,
		       coherence_score, route_signature, updated_at
		FROM spectral_signatures
		WHERE vehicle_id = ?
	`, vehicleID)

	var (
		anomalyScore float64
		dominantFreq float64
		entropy      float64
		highBand     float64
		lowBand      float64
		midBand      float64
		coherence    float64
		sigBlob      []byte
		updatedAt    int64
	)
	err := row.Scan(
		&anomalyScore, &dominantFreq, &entropy,
		&highBand, &lowBand, &midBand,
		&coherence, &sigBlob, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load spectral signature (vehicle=%s): %w", vehicleID, err)
	}

	profile := spectral.NewSpectralProfile()
	profile.AnomalyScore = float32(anomalyScore)
	profile.LastUpdatedNS = updatedAt * int64(time.Second)
	profile.RouteSignature = decodeFloat32Slice(sigBlob)
	profile.Features = spectral.SpectralFeatures{
		DominantFrequency: dominantFreq,
		SpectralEntropy:   entropy,
		HighBandEnergy:    highBand,
		LowBandEnergy:     lowBand,
		MidBandEnergy:     midBand,
		CoherenceScore:    coherence,
	}
	return profile, nil
}

// GetHighAnomalyVehicles returns vehicle IDs whose persisted anomaly score
// meets or exceeds minScore, ordered by score descending, capped at limit.
// Useful for bootstrapping the attention engine after a restart — vehicles
// with a historically high spectral anomaly score can be surfaced immediately
// without waiting for their ring buffers to refill.
func (p *Persistor) GetHighAnomalyVehicles(minScore float32, limit int) ([]string, error) {
	rows, err := p.db.Query(`
		SELECT vehicle_id
		FROM spectral_signatures
		WHERE anomaly_score >= ?
		ORDER BY anomaly_score DESC, updated_at DESC
		LIMIT ?
	`, float64(minScore), limit)
	if err != nil {
		return nil, fmt.Errorf("query high anomaly vehicles: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan vehicle id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Close releases the database connection.
func (p *Persistor) Close() error { return p.db.Close() }

// ─── Blob helpers ─────────────────────────────────────────────────────────────

// encodeFloat32Slice serialises vals as a little-endian byte slice.
// Each float32 occupies exactly 4 bytes.
func encodeFloat32Slice(vals []float32) []byte {
	buf := make([]byte, len(vals)*4)
	for i, v := range vals {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// decodeFloat32Slice deserialises a little-endian byte slice produced by
// encodeFloat32Slice. Returns nil for empty or nil input.
func decodeFloat32Slice(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	n := len(data) / 4
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return out
}
