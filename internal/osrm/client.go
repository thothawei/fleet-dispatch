package osrmclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	if c == nil {
		return fallbackETA(fromLat, fromLng, toLat, toLng)
	}
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

// Point 路徑上的一個座標點（N5）。
// 自訂而非用 model.GeoPoint：osrm 是最底層的外部服務 client，不該依賴領域模型。
type Point struct {
	Lat, Lng float64
}

// ErrTooFewPoints 多點路線至少要兩個座標點。
var ErrTooFewPoints = errors.New("路線至少需要兩個座標點")

// RouteVia 計算「依序經過所有點」的全程路線（N5）：起點 → 各停靠點 → 終點，**含繞路**。
//
// OSRM 的 /route/v1 本來就支援多座標（`;` 分隔），是我們的 client 之前只開放兩點。
// 既有的兩點 RouteDuration **保留不動**——派單 ETA 與 F3 的既有行為不受影響。
//
// 失敗時退回逐段 haversine 累加（見 fallbackVia）。
func (c *Client) RouteVia(ctx context.Context, points []Point) (durationSec int, distanceM int, err error) {
	if len(points) < 2 {
		return 0, 0, ErrTooFewPoints
	}
	if c == nil {
		return fallbackVia(points)
	}
	coords := make([]string, 0, len(points))
	for _, p := range points {
		// OSRM 的座標順序是 lng,lat——對調會把台北送到別的半球。
		coords = append(coords, fmt.Sprintf("%f,%f", p.Lng, p.Lat))
	}
	url := fmt.Sprintf("%s/route/v1/driving/%s?overview=false", c.baseURL, strings.Join(coords, ";"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fallbackVia(points)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fallbackVia(points)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fallbackVia(points)
	}
	var data routeResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Code != "Ok" || len(data.Routes) == 0 {
		return fallbackVia(points)
	}
	return int(data.Routes[0].Duration), int(data.Routes[0].Distance), nil
}

// fallbackVia OSRM 不可用時的多點退路：**逐段**累加再取和（N5 明確要求）。
//
// 不可以只算頭尾兩點——那會把繞路全部漏掉，多停靠點行程的退路里程會退化成直達距離；
// 而 billableDistanceM 取「軌跡 vs 路線」大者時就只剩軌跡可用，軌跡若稀疏（見 F3）
// 就會嚴重低估車資。
func fallbackVia(points []Point) (int, int, error) {
	if len(points) < 2 {
		return 0, 0, ErrTooFewPoints
	}
	total := 0.0
	for i := 1; i < len(points); i++ {
		total += util.HaversineM(points[i-1].Lat, points[i-1].Lng, points[i].Lat, points[i].Lng) * 1.4
	}
	return util.EstimateETASec(total), int(total), nil
}
