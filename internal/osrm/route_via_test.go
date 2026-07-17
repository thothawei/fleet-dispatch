package osrmclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 台北四個點，構成一趟繞路行程。
var (
	taipeiMain = Point{Lat: 25.0478, Lng: 121.5170} // 台北車站
	cityHall   = Point{Lat: 25.0339, Lng: 121.5645} // 市政府
	songshan   = Point{Lat: 25.0697, Lng: 121.5522} // 松山機場
	taipei101  = Point{Lat: 25.0339, Lng: 121.5645} // 台北101
)

// TestRouteVia_URL串接所有座標 OSRM 的 /route/v1 支援多座標（`;` 分隔），
// 之前只是我們的 client 沒開放。
func TestRouteVia_URL串接所有座標(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":   "Ok",
			"routes": []map[string]any{{"duration": 1234.0, "distance": 8888.0}},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	sec, distM, err := c.RouteVia(context.Background(), []Point{taipeiMain, cityHall, songshan})
	if err != nil {
		t.Fatalf("RouteVia 失敗：%v", err)
	}
	if sec != 1234 || distM != 8888 {
		t.Fatalf("應回 OSRM 的數值，得到 %d 秒 / %d 公尺", sec, distM)
	}

	// 座標順序必須是 lng,lat——對調會把台北送到別的半球。
	wantCoords := "121.517000,25.047800;121.564500,25.033900;121.552200,25.069700"
	if !strings.Contains(gotPath, wantCoords) {
		t.Fatalf("URL 應依序串接所有座標（lng,lat）：\n得到 %s\n預期含 %s", gotPath, wantCoords)
	}
}

// TestRouteVia_點數不足 少於兩點無法構成路線。
func TestRouteVia_點數不足(t *testing.T) {
	c := NewClient("http://unused")
	for _, pts := range [][]Point{nil, {taipeiMain}} {
		if _, _, err := c.RouteVia(context.Background(), pts); !errors.Is(err, ErrTooFewPoints) {
			t.Fatalf("預期 ErrTooFewPoints，得到 %v", err)
		}
	}
}

// TestRouteVia_OSRM掛掉時逐段累加 N5 明確要求：退路要**逐段**算再取和。
//
// 只算頭尾兩點會把繞路全部漏掉——多停靠點的退路里程退化成直達距離，
// billableDistanceM 取大者時就只剩軌跡可用，軌跡稀疏（F3）就會嚴重低估車資。
func TestRouteVia_OSRM掛掉時逐段累加(t *testing.T) {
	// 500 → 走 fallback。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)

	// 繞路：台北車站 → 松山機場 → 台北101（先北再南）。
	viaRoute := []Point{taipeiMain, songshan, taipei101}
	_, viaM, err := c.RouteVia(context.Background(), viaRoute)
	if err != nil {
		t.Fatalf("fallback 不該失敗：%v", err)
	}

	// 直達：台北車站 → 台北101。
	_, directM, err := c.RouteVia(context.Background(), []Point{taipeiMain, taipei101})
	if err != nil {
		t.Fatalf("fallback 不該失敗：%v", err)
	}

	if viaM <= directM {
		t.Fatalf("繞路里程(%d) 應大於直達(%d)——逐段累加沒生效，繞路被漏掉了", viaM, directM)
	}

	// 逐段和：手動累加兩段，應與 RouteVia 的結果一致。
	_, segA, _ := fallbackVia([]Point{taipeiMain, songshan})
	_, segB, _ := fallbackVia([]Point{songshan, taipei101})
	if diff := viaM - (segA + segB); diff > 1 || diff < -1 {
		t.Fatalf("多點退路應等於逐段和：得到 %d，逐段和 %d", viaM, segA+segB)
	}
}

// TestRouteVia_nil客戶端走退路 與既有 RouteDuration 的 nil 行為一致。
func TestRouteVia_nil客戶端走退路(t *testing.T) {
	var c *Client
	_, distM, err := c.RouteVia(context.Background(), []Point{taipeiMain, songshan})
	if err != nil {
		t.Fatalf("nil client 應走退路而非報錯：%v", err)
	}
	if distM <= 0 {
		t.Fatalf("退路里程應為正數，得到 %d", distM)
	}
}
