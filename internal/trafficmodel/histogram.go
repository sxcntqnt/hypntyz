package trafficmodel

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

	// DefaultExpectedSpeedMS is returned when no hourly data exists yet.
	// 15 m/s ≈ 54 km/h — a conservative urban arterial default.
	DefaultExpectedSpeedMS = 15.0
)

// SpeedHistogram stores speed observations bucketed by hour-of-week and speed
// bin. The two-dimensional key is packed into a single uint16 so the hot path
// (AddSample) needs only one map write.
type SpeedHistogram struct {
	mu         sync.RWMutex
	bins       map[uint16]int64 // packed (hour*120 + speedBin) → count
	totalCount int64
	totalSum   float64 // sum of raw m/s values, for fast mean computation
}

// NewSpeedHistogram allocates an empty histogram.
func NewSpeedHistogram() *SpeedHistogram {
	return &SpeedHistogram{bins: make(map[uint16]int64)}
}

// PackBin packs an (hour-of-week, speed-bin) pair into the uint16 key used
// internally. Both inputs are clamped to valid ranges.
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
		hourOfWeek += HoursInWeek
	}
	speedBin := int(math.Round(speedMS * 3.6 / SpeedBinSizeKmh))

	h.mu.Lock()
	h.bins[PackBin(hourOfWeek, speedBin)]++
	h.totalCount++
	h.totalSum += speedMS
	h.mu.Unlock()
}

// GetMean returns the overall mean speed in m/s. Returns 0 if empty.
func (h *SpeedHistogram) GetMean() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.totalCount == 0 {
		return 0
	}
	return h.totalSum / float64(h.totalCount)
}

// GetExpectedSpeed returns the mean speed in m/s for the given hour-of-week.
// Falls back to DefaultExpectedSpeedMS when no data exists for that hour.
func (h *SpeedHistogram) GetExpectedSpeed(hourOfWeek int) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var total float64
	var count int64
	for bin, cnt := range h.bins {
		if int(bin)/NumSpeedBins == hourOfWeek {
			speedMS := float64(int(bin)%NumSpeedBins) * SpeedBinSizeKmh / 3.6
			total += speedMS * float64(cnt)
			count += cnt
		}
	}
	if count == 0 {
		return DefaultExpectedSpeedMS
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
		speed := float64(int(bin)%NumSpeedBins) * SpeedBinSizeKmh / 3.6
		diff := speed - mean
		variance += diff * diff * float64(cnt)
	}
	return h.totalCount, mean, math.Sqrt(variance/float64(h.totalCount))
}

// Merge incorporates all samples from other into h. Both histograms are
// locked independently so concurrent writes to either remain safe.
func (h *SpeedHistogram) Merge(other *SpeedHistogram) {
	other.mu.RLock()
	otherBins := make(map[uint16]int64, len(other.bins))
	for k, v := range other.bins {
		otherBins[k] = v
	}
	otherCount, otherSum := other.totalCount, other.totalSum
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

// ExportBins returns a copy of the internal bin map safe for use outside the
// histogram's lock (e.g. for persistence).
func (h *SpeedHistogram) ExportBins() map[uint16]int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uint16]int64, len(h.bins))
	for k, v := range h.bins {
		out[k] = v
	}
	return out
}
