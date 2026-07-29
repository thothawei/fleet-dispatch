package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"

	"line-fleet-dispatch/internal/config"
)

func TestNewLogger_ProductionOutputsStructuredJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	lg := newLogger(&config.Config{AppEnv: "production"}, buf)

	lg.Info().Int64("ride_id", 42).Int64("duration_ms", 137).Msg("已派單")

	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil {
		t.Fatalf("production 應輸出合法 JSON，實得：%s", buf.String())
	}
	// 重點不是「有輸出」，而是欄位仍是獨立的、可被日誌平台當條件查詢。
	if m["ride_id"] != float64(42) {
		t.Fatalf("ride_id 應為獨立欄位，實得：%v", m)
	}
	if m["duration_ms"] != float64(137) {
		t.Fatalf("duration_ms 應為獨立欄位，實得：%v", m)
	}
	if m["message"] != "已派單" {
		t.Fatalf("message 欄位不正確：%v", m)
	}
	if _, ok := m["time"]; !ok {
		t.Fatalf("缺少 time 欄位，日誌平台無法排序：%v", m)
	}
}

func TestNewLogger_NonProductionIsHumanReadable(t *testing.T) {
	for _, env := range []string{"local", "staging", ""} {
		t.Run("env="+env, func(t *testing.T) {
			buf := &bytes.Buffer{}
			lg := newLogger(&config.Config{AppEnv: env}, buf)

			lg.Info().Int64("ride_id", 42).Msg("已派單")

			var m map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err == nil {
				t.Fatalf("非 production 預期為人類可讀格式，卻是 JSON：%s", buf.String())
			}
			if !bytes.Contains(buf.Bytes(), []byte("已派單")) {
				t.Fatalf("輸出應含訊息本文，實得：%s", buf.String())
			}
		})
	}
}

func TestSetupLogger_LevelFiltering(t *testing.T) {
	origLevel := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })

	setupLogger(&config.Config{AppEnv: "production", LogLevel: "error"})
	if got := zerolog.GlobalLevel(); got != zerolog.ErrorLevel {
		t.Fatalf("LOG_LEVEL=error 應套用 error 級別，實得 %s", got)
	}

	// 用獨立 buffer 驗證過濾真的生效（全域級別對所有 logger 一體適用）。
	buf := &bytes.Buffer{}
	lg := newLogger(&config.Config{AppEnv: "production"}, buf)

	lg.Info().Msg("這筆應被過濾")
	if buf.Len() != 0 {
		t.Fatalf("info 不應輸出，實得：%s", buf.String())
	}

	// 校準：同一條斷言路徑在 error 級別確實寫得出來，
	// 證明上面的空值是被級別擋掉，而不是 logger 根本沒接上 buffer。
	lg.Error().Msg("這筆應輸出")
	if buf.Len() == 0 {
		t.Fatal("校準失敗：error 級別也沒有輸出，前一項斷言不具意義")
	}
}

func TestSetupLogger_InvalidLevelFallsBackToInfo(t *testing.T) {
	origLevel := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })

	setupLogger(&config.Config{AppEnv: "production", LogLevel: "not-a-level"})

	if got := zerolog.GlobalLevel(); got != zerolog.InfoLevel {
		t.Fatalf("無法解析的 LOG_LEVEL 應退回 info，實得 %s", got)
	}
}
