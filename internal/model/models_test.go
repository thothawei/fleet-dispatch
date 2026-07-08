package model

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"testing"
)

// ewkbPoint 依 EWKB 規格「獨立」組出一個帶 SRID 的 Point，供測試 GeoPoint.Scan 用。
// 刻意不呼叫被測程式碼自己的編碼器，避免自證式測試。
func ewkbPoint(lng, lat float64, littleEndian bool) []byte {
	buf := new(bytes.Buffer)
	var bo binary.ByteOrder
	if littleEndian {
		buf.WriteByte(1)
		bo = binary.LittleEndian
	} else {
		buf.WriteByte(0)
		bo = binary.BigEndian
	}
	_ = binary.Write(buf, bo, uint32(0x20000001)) // SRID flag(0x20000000) | Point(1)
	_ = binary.Write(buf, bo, uint32(4326))
	_ = binary.Write(buf, bo, math.Float64bits(lng))
	_ = binary.Write(buf, bo, math.Float64bits(lat))
	return buf.Bytes()
}

const geoEps = 1e-9

func TestGeoPointScan_小端位元組(t *testing.T) {
	var g GeoPoint
	if err := g.Scan(ewkbPoint(121.5654, 25.0330, true)); err != nil {
		t.Fatalf("Scan 失敗：%v", err)
	}
	if math.Abs(g.Lng-121.5654) > geoEps || math.Abs(g.Lat-25.0330) > geoEps {
		t.Fatalf("解析座標錯誤：得到 lng=%f lat=%f", g.Lng, g.Lat)
	}
}

func TestGeoPointScan_大端位元組(t *testing.T) {
	var g GeoPoint
	if err := g.Scan(ewkbPoint(121.5654, 25.0330, false)); err != nil {
		t.Fatalf("Scan 失敗：%v", err)
	}
	if math.Abs(g.Lng-121.5654) > geoEps || math.Abs(g.Lat-25.0330) > geoEps {
		t.Fatalf("解析座標錯誤：得到 lng=%f lat=%f", g.Lng, g.Lat)
	}
}

// pgx 讀 geography 欄位預設回傳 EWKB 的十六進位字串，需一併支援
func TestGeoPointScan_十六進位字串(t *testing.T) {
	hexStr := hex.EncodeToString(ewkbPoint(121.5654, 25.0330, true))
	var g GeoPoint
	if err := g.Scan(hexStr); err != nil {
		t.Fatalf("Scan 失敗：%v", err)
	}
	if math.Abs(g.Lng-121.5654) > geoEps || math.Abs(g.Lat-25.0330) > geoEps {
		t.Fatalf("解析座標錯誤：得到 lng=%f lat=%f", g.Lng, g.Lat)
	}
}

// 十六進位字串也可能以 []byte 形式送進 Scan
func TestGeoPointScan_十六進位位元組(t *testing.T) {
	hexBytes := []byte(hex.EncodeToString(ewkbPoint(121.5654, 25.0330, true)))
	var g GeoPoint
	if err := g.Scan(hexBytes); err != nil {
		t.Fatalf("Scan 失敗：%v", err)
	}
	if math.Abs(g.Lng-121.5654) > geoEps || math.Abs(g.Lat-25.0330) > geoEps {
		t.Fatalf("解析座標錯誤：得到 lng=%f lat=%f", g.Lng, g.Lat)
	}
}

func TestGeoPointScan_nil為no_op(t *testing.T) {
	g := GeoPoint{Lat: 1, Lng: 2}
	if err := g.Scan(nil); err != nil {
		t.Fatalf("nil 應為 no-op，卻回錯誤：%v", err)
	}
	if g.Lat != 1 || g.Lng != 2 {
		t.Fatalf("nil Scan 不應改動既有值")
	}
}

func TestGeoPointScan_壞資料回錯誤(t *testing.T) {
	var g GeoPoint
	if err := g.Scan([]byte{0x01, 0x02}); err == nil {
		t.Fatalf("過短的 EWKB 應回錯誤")
	}
}
