package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"
)

// 台北市大致範圍
const (
	taipeiLatMin = 25.00
	taipeiLatMax = 25.10
	taipeiLngMin = 121.50
	taipeiLngMax = 121.60
)

func main() {
	count := flag.Int("n", 20, "模擬司機數量")
	apiURL := flag.String("api", getenv("API_URL", "http://localhost:8080"), "後端 API URL")
	interval := flag.Duration("interval", 8*time.Second, "位置回報間隔")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())
	client := &http.Client{Timeout: 10 * time.Second}

	type driverState struct {
		ID    int64
		Token string
		Lat   float64
		Lng   float64
	}

	var drivers []driverState
	for i := 0; i < *count; i++ {
		lineUserID := fmt.Sprintf("sim-driver-%03d", i+1)
		name := fmt.Sprintf("模擬司機%d", i+1)

		id, token, err := registerDriver(client, *apiURL, lineUserID, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "註冊司機 %s 失敗: %v\n", name, err)
			continue
		}
		drivers = append(drivers, driverState{
			ID:    id,
			Token: token,
			Lat:   randFloat(taipeiLatMin, taipeiLatMax),
			Lng:   randFloat(taipeiLngMin, taipeiLngMax),
		})
		fmt.Printf("已註冊司機 #%d (%s)\n", id, name)
	}

	fmt.Printf("共 %d 台模擬車，每 %v 回報位置...\n", len(drivers), *interval)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		for i := range drivers {
			d := &drivers[i]
			d.Lat += randFloat(-0.001, 0.001)
			d.Lng += randFloat(-0.001, 0.001)
			d.Lat = clamp(d.Lat, taipeiLatMin, taipeiLatMax)
			d.Lng = clamp(d.Lng, taipeiLngMin, taipeiLngMax)

			if err := reportLocation(client, *apiURL, d.Token, d.Lat, d.Lng); err != nil {
				fmt.Fprintf(os.Stderr, "司機 #%d 回報失敗: %v\n", d.ID, err)
			}
		}
		<-ticker.C
	}
}

func registerDriver(client *http.Client, apiURL, lineUserID, name string) (int64, string, error) {
	body, _ := json.Marshal(map[string]string{
		"line_user_id": lineUserID,
		"name":         name,
		"password":     "sim-password",
	})
	resp, err := client.Post(apiURL+"/api/driver/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result struct {
		DriverID int64  `json:"driver_id"`
		Token    string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, "", err
	}
	return result.DriverID, result.Token, nil
}

func reportLocation(client *http.Client, apiURL, token string, lat, lng float64) error {
	body, _ := json.Marshal(map[string]interface{}{
		"lat": lat,
		"lng": lng,
	})
	req, err := http.NewRequest(http.MethodPost, apiURL+"/api/driver/location", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func randFloat(min, max float64) float64 {
	return min + rand.Float64()*(max-min)
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
