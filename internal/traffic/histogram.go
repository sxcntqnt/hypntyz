package traffic

import (
	"math"
	"sync"
)

const (
	// HoursInWeek is the number of distinct hour-of-week slots (24 × 7).
	HoursInWeek = 168

	// NumSpeedBins is the number of speed buckets (0–119 km/h).
	NumSpeedBins = 120

	// SpeedBinSizeKmh is the width of each speed bucket in km/h.
	SpeedBinSizeKmh = 1.0

	// maxTrackedSpeedKmh is the ceiling of the last speed bucket.
	maxTrackedSpeedKmh = float64(NumSpeedBins) * SpeedBinSizeKmh // 120 km/h

	// defaultExpectedSpeedMS is returned when no hourly data exists yet.
	// 15 m/s ≈ 54 km/h — a conservative urban arterial default.
	defaultExpectedSpeedMS = 15.0
)

// SpeedHistogram stores speed observations bucketed by hour-of-week and
// speed bin. The two-dimensional key is packed into a single uint16 so the
// hot path (AddSample) needs only one map write.
type SpeedHistogram struct {
	mu         sync.RWMutex
	bins       map[uint16]int64 // packed (hour*120 + speedBin) → count
	totalCount int64
	totalSum   float64 // sum of raw m/s values, for fast mean computation
}

// NewSpeedHistogram allocates an empty histogram.
func NewSpeedHistogram() *SpeedHistogram {
	return &SpeedHistogram{
		bins: make(map[uint16]int64),
	}
}

// PackBin packs an (hour-of-week, speed-bin) pair into the uint16 key used
// internally. Inputs are clamped to valid ranges.
func PackBin(hour, speedBin int) uint16 {
	if hour < 0 {
		hour = 0
	} else if hour >= HoursInWeek {
		hour = HoursInWeek - 1
	}
	if speedBin < 0 {
		speedBin = 0
	} else if speedBin >= NumSpeedBins {
		speedBin = NumSpeedBins - 1
	}
	return uint16(hour*NumSpeedBins + speedBin)
}

// AddSample records one speed observation. timeNS is a Unix nanosecond
// timestamp used to derive the hour-of-week; speedMS is in m/s.
func (h *SpeedHistogram) AddSample(timeNS int64, speedMS float64) {
	hourOfWeek := int((timeNS/int64(3600*1e9))%HoursInWeek)
	if hourOfWeek < 0 {
		hourOfWeek += HoursInWeek // handle negative timestamps gracefully
	}

	speedKmh := speedMS * 3.6
	speedBin := int(math.Round(speedKmh / SpeedBinSizeKmh))

	h.mu.Lock()
	h.bins[PackBin(hourOfWeek, speedBin)]++
	h.totalCount++
	h.totalSum += speedMS
	h.mu.Unlock()
}

// GetMean returns the overall mean speed in m/s across all samples.
// Returns 0 if no samples have been recorded.
func (h *SpeedHistogram) GetMean() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.totalCount == 0 {
		return 0
	}
	return h.totalSum / float64(h.totalCount)
}

// GetExpectedSpeed returns the mean speed in m/s for a specific hour-of-week
// derived from the historical distribution. Falls back to
// defaultExpectedSpeedMS when there is no data for that hour.
func (h *SpeedHistogram) GetExpectedSpeed(hourOfWeek int) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var total float64
	var count int64
	for bin, cnt := range h.bins {
		if int(bin)/NumSpeedBins == hourOfWeek {
			speedBin := int(bin) % NumSpeedBins
			speedMS := float64(speedBin) * SpeedBinSizeKmh / 3.6
			total += speedMS * float64(cnt)
			count += cnt
		}
	}
	if count == 0 {
		return defaultExpectedSpeedMS
	}
	return total / float64(count)
}

// GetStats returns (sampleCount, meanSpeedMS, stdDevMS).
// All three are zero when the histogram is empty.
func (h *SpeedHistogram) GetStats() (count int64, mean, stddev float64) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.totalCount == 0 {
		return 0, 0, 0
	}
	mean = h.totalSum / float64(h.totalCount)

	var variance float64
	for bin, cnt := range h.bins {
		speedBin := int(bin) % NumSpeedBins
		speed := float64(speedBin) * SpeedBinSizeKmh / 3.6
		diff := speed - mean
		variance += diff * diff * float64(cnt)
	}
	variance /= float64(h.totalCount)

	return h.totalCount, mean, math.Sqrt(variance)
}

// Merge incorporates all samples from other into h.
// other is read-locked during the merge so concurrent writes to other are safe.
func (h *SpeedHistogram) Merge(other *SpeedHistogram) {
	other.mu.RLock()
	otherBins := make(map[uint16]int64, len(other.bins))
	for k, v := range other.bins {
		otherBins[k] = v
	}
	otherCount := other.totalCount
	otherSum := other.totalSum
	other.mu.RUnlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	for k, v := range otherBins {
		h.bins[k] += v
	}
	h.totalCount += otherCount
	h.totalSum += otherSum
}

// Reset clears all samples.
func (h *SpeedHistogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bins = make(map[uint16]int64)
	h.totalCount = 0
	h.totalSum = 0
}

// ExportBins returns a copy of the internal bin map, safe for use outside the
// histogram's lock (e.g. for persistence). Keys are packed uint16 values as
// produced by PackBin.
func (h *SpeedHistogram) ExportBins() map[uint16]int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uint16]int64, len(h.bins))
	for k, v := range h.bins {
		out[k] = v
	}
	return out
}
