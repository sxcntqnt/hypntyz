package sirtebasin

import (
	"encoding/json"
	"net/http"
)

type Client struct {
	BaseURL string
}

type Vehicle struct {
	ID     string  `json:"id"`
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Speed  float64 `json:"speed"`
	Head   float64 `json:"heading"`
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL}
}

func (c *Client) QueryViewport(minLat, minLon, maxLat, maxLon float64) ([]Vehicle, error) {
	url := c.BaseURL + "/query/viewport"

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var vehicles []Vehicle
	err = json.NewDecoder(resp.Body).Decode(&vehicles)

	return vehicles, err
}
