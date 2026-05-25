package traffic

import (
	"math"
	"sync"
)

// SpeedHistogram stores speed observations bucketed by hour-of-week and speed bin
// Modeled after SegmentStatistics in traffic-engine
type SpeedHistogram struct {
	mu          sync.RWMutex
	bins        map[uint16]int64 // Packed bin -> count
	totalCount  int64
	totalSum    float64
}

const (
	HoursInWeek   = 168 // 24 * 7
	NumSpeedBins  = 120 // 0-120 km/h
	SpeedBinSize  = 1.0 // km/h per bin
	MaxTrackedSpeed = 120.0 // km/h
)

func NewSpeedHistogram() *SpeedHistogram {
	return &SpeedHistogram{
		bins: make(map[uint16]int64),
	}
}

// GetHourSpeedBin packs hour and speed into a single uint16 key
// hour: 0-167 (168 hours in a week)
// speedBin: 0-119 (0-120 km/h)
func GetHourSpeedBin(hour int, speedBin int) uint16 {
	if hour > 167 {
		hour = 167
	}
	if speedBin > 119 {
		speedBin = 119
	}
	return uint16(hour*120 + speedBin)
}

// AddSample adds a speed sample to the histogram
func (h *SpeedHistogram) AddSample(timeNS int64, speedMS float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Convert time to hour-of-week
	// (Simplified: assumes UTC, can be enhanced with timezone support)
	hourOfWeek := int((timeNS / int64(3600*1e9)) % 168)

	// Convert speed to bin (m/s -> km/h)
	speedKmh := speedMS * 3.6
	speedBin := int(math.Round(speedKmh / SpeedBinSize))
	if speedBin < 0 {
		speedBin = 0
	}
	if speedBin >= NumSpeedBins {
		speedBin = NumSpeedBins - 1
	}

	bin := GetHourSpeedBin(hourOfWeek, speedBin)
	h.bins[bin]++
	h.totalCount++
	h.totalSum += speedMS
}

// GetMean returns the mean speed for the entire histogram
func (h *SpeedHistogram) GetMean() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.totalCount == 0 {
		return 0
	}
	return h.totalSum / float64(h.totalCount)
}

// GetExpectedSpeed returns the expected speed for a given hour
func (h *SpeedHistogram) GetExpectedSpeed(hourOfWeek int) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	total := 0.0
	count := int64(0)

	for bin, cnt := range h.bins {
		hour := int(bin) / 120
		if hour == hourOfWeek {
			speedBin := int(bin) % 120
			total += float64(speedBin) * SpeedBinSize / 3.6 * float64(cnt) // Convert back to m/s
			count += cnt
		}
	}

	if count == 0 {
		return 15.0 // Default 15 m/s (~54 km/h) if no data
	}
	return total / float64(count)
}

// GetStats returns count, mean, and stddev
func (h *SpeedHistogram) GetStats() (int64, float64, float64) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.totalCount == 0 {
		return 0, 0, 0
	}

	mean := h.totalSum / float64(h.totalCount)

	// Compute stddev
	variance := 0.0
	for bin, cnt := range h.bins {
		speedBin := int(bin) % 120
		speed := float64(speedBin) * SpeedBinSize / 3.6
		diff := speed - mean
		variance += diff * diff * float64(cnt)
	}
	variance /= float64(h.totalCount)
	stddev := math.Sqrt(variance)

	return h.totalCount, mean, stddev
}

// Merge combines two histograms
func (h *SpeedHistogram) Merge(other *SpeedHistogram) {
	h.mu.Lock()
	defer h.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for bin, cnt := range other.bins {
		h.bins[bin] += cnt
	}
	h.totalCount += other.totalCount
	h.totalSum += other.totalSum
}

// Reset clears the histogram
func (h *SpeedHistogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bins = make(map[uint16]int64)
	h.totalCount = 0
	h.totalSum = 0
}
