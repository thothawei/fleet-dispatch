package osrmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"line-fleet-dispatch/internal/util"
)

// Client OSRM 路徑引擎 client
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

type routeResponse struct {
	Routes []struct {
		Duration float64 `json:"duration"`
		Distance float64 `json:"distance"`
	} `json:"routes"`
	Code string `json:"code"`
}

// RouteDuration 計算兩點路徑時間（秒）與距離（公尺）；失敗時用直線估算
func (c *Client) RouteDuration(ctx context.Context, fromLat, fromLng, toLat, toLng float64) (durationSec int, distanceM int, err error) {
	url := fmt.Sprintf("%s/route/v1/driving/%f,%f;%f,%f?overview=false",
		c.baseURL, fromLng, fromLat, toLng, toLat)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fallbackETA(fromLat, fromLng, toLat, toLng)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fallbackETA(fromLat, fromLng, toLat, toLng)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fallbackETA(fromLat, fromLng, toLat, toLng)
	}

	var data routeResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Code != "Ok" || len(data.Routes) == 0 {
		return fallbackETA(fromLat, fromLng, toLat, toLng)
	}

	return int(data.Routes[0].Duration), int(data.Routes[0].Distance), nil
}

func fallbackETA(fromLat, fromLng, toLat, toLng float64) (int, int, error) {
	dist := util.HaversineM(fromLat, fromLng, toLat, toLng)
	// 路網係數 1.4 近似彎路
	roadDist := dist * 1.4
	return util.EstimateETASec(roadDist), int(roadDist), nil
}
