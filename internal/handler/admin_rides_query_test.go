package handler

import (
	"net/url"
	"testing"

	"line-fleet-dispatch/internal/repository"
)

// parseRideListFilter 是純函式（不碰 DB），CI 無 Docker 也能跑。

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("query 組裝失敗: %v", err)
	}
	return v
}

func TestParseRideListFilter_空查詢為全預設(t *testing.T) {
	f, err := parseRideListFilter(url.Values{})
	if err != nil {
		t.Fatalf("不該出錯: %v", err)
	}
	if f.Status != nil || f.From != "" || f.To != "" || f.Q != "" {
		t.Fatalf("不該有篩選條件: %+v", f)
	}
	// Limit 留 0 交給 repository 套預設，避免預設值散落兩處
	if f.Limit != 0 || f.Offset != 0 {
		t.Fatalf("limit/offset 應為零值: %+v", f)
	}
}

func TestParseRideListFilter_完整參數(t *testing.T) {
	f, err := parseRideListFilter(mustQuery(t, "status=4&limit=20&offset=40&from=2026-07-01&to=2026-07-10&q=%E5%8F%B0%E5%8C%97"))
	if err != nil {
		t.Fatalf("不該出錯: %v", err)
	}
	if f.Status == nil || *f.Status != 4 {
		t.Fatalf("status 應為 4: %+v", f.Status)
	}
	if f.Limit != 20 || f.Offset != 40 {
		t.Fatalf("limit/offset 解析錯: %d/%d", f.Limit, f.Offset)
	}
	if f.From != "2026-07-01" || f.To != "2026-07-10" {
		t.Fatalf("日期解析錯: %s ~ %s", f.From, f.To)
	}
	if f.Q != "台北" {
		t.Fatalf("q 解析錯: %q", f.Q)
	}
}

func TestParseRideListFilter_關鍵字去除前後空白(t *testing.T) {
	f, err := parseRideListFilter(mustQuery(t, "q=++%E5%8F%B0%E5%8C%97++"))
	if err != nil {
		t.Fatalf("不該出錯: %v", err)
	}
	if f.Q != "台北" {
		t.Fatalf("q 應去空白: %q", f.Q)
	}
}

func TestParseRideListFilter_錯誤參數回錯(t *testing.T) {
	cases := map[string]string{
		"status 非整數": "status=abc",
		"limit 非整數":  "limit=abc",
		"limit 為 0":  "limit=0",
		"limit 超過上限": "limit=501",
		"offset 非整數": "offset=abc",
		"offset 為負":  "offset=-1",
		"from 格式錯":   "from=2026%2F07%2F01",
		"to 格式錯":     "to=07-10-2026",
		"from 晚於 to": "from=2026-07-11&to=2026-07-10",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRideListFilter(mustQuery(t, raw)); err == nil {
				t.Fatalf("%s 應回錯誤，卻通過了", name)
			}
		})
	}
}

func TestParseRideListFilter_limit邊界值可接受(t *testing.T) {
	for _, raw := range []string{"limit=1", "limit=500"} {
		f, err := parseRideListFilter(mustQuery(t, raw))
		if err != nil {
			t.Fatalf("%s 應可接受: %v", raw, err)
		}
		if f.Limit != 1 && f.Limit != repository.RideListMaxLimit {
			t.Fatalf("limit 解析錯: %d", f.Limit)
		}
	}
}

func TestParseRideListFilter_同日區間可接受(t *testing.T) {
	f, err := parseRideListFilter(mustQuery(t, "from=2026-07-10&to=2026-07-10"))
	if err != nil {
		t.Fatalf("同一天的區間應可接受: %v", err)
	}
	if f.From != f.To {
		t.Fatalf("from/to 應相同: %s %s", f.From, f.To)
	}
}
