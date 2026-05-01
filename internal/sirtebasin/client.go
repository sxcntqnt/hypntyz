package sirtebasin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"hypnotz/internal/types"
)

type Client struct {
	BaseURL    string
	httpClient *http.Client
}

type VehicleResponse struct {
	ID        string  `json:"id"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Speed     float64 `json:"speed"`
	Heading   float64 `json:"heading"`
	Timestamp int64   `json:"timestamp"`
	FleetID   string  `json:"fleet_id,omitempty"`
	Type      string  `json:"type,omitempty"`
	Anomaly   bool    `json:"anomaly,omitempty"`
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) QueryViewport(ctx context.Context, minLat, minLon, maxLat, maxLon float64) ([]types.Vehicle, error) {
	url := fmt.Sprintf("%s/query/viewport?min_lat=%f&min_lon=%f&max_lat=%f&max_lon=%f",
		c.BaseURL, minLat, minLon, maxLat, maxLon)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sirtebasin query failed: %d", resp.StatusCode)
	}

	var respVehicles []VehicleResponse
	if err := json.NewDecoder(resp.Body).Decode(&respVehicles); err != nil {
		return nil, err
	}

	vehicles := make([]types.Vehicle, len(respVehicles))
	for i, v := range respVehicles {
		vehicles[i] = types.Vehicle{
			ID:        v.ID,
			Lat:       v.Lat,
			Lon:       v.Lon,
			Speed:     v.Speed,
			Heading:   v.Heading,
			Timestamp: time.Unix(v.Timestamp, 0),
			FleetID:   v.FleetID,
			Type:      v.Type,
			Anomaly:   v.Anomaly,
		}
	}

	return vehicles, nil
}

func (c *Client) FetchDelta(ctx context.Context, region string, since int64) ([]types.Vehicle, error) {
	url := fmt.Sprintf("%s/delta/%s?since=%d", c.BaseURL, region, since)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sirtebasin delta failed: %d", resp.StatusCode)
	}

	var respVehicles []VehicleResponse
	if err := json.NewDecoder(resp.Body).Decode(&respVehicles); err != nil {
		return nil, err
	}

	vehicles := make([]types.Vehicle, len(respVehicles))
	for i, v := range respVehicles {
		vehicles[i] = types.Vehicle{
			ID:        v.ID,
			Lat:       v.Lat,
			Lon:       v.Lon,
			Speed:     v.Speed,
			Heading:   v.Heading,
			Timestamp: time.Unix(v.Timestamp, 0),
			FleetID:   v.FleetID,
			Type:      v.Type,
			Anomaly:   v.Anomaly,
		}
	}

	return vehicles, nil
}

func (c *Client) Health(ctx context.Context) error {
	url := fmt.Sprintf("%s/health", c.BaseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sirtebasin health check failed: %d", resp.StatusCode)
	}

	return nil
}
