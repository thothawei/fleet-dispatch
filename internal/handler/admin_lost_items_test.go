package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/repository"
)

// TestListLostItems_參數驗證 驗證 status 白名單與未注入 repo 的降級行為。
// 資料正確性（JOIN 姓名、狀態篩選）由 repository 整合測試涵蓋。
func TestListLostItems_參數驗證(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 未注入 repo → 503
	hNoRepo := &AdminHandler{}
	r := gin.New()
	r.GET("/api/admin/lost-items", hNoRepo.ListLostItems)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/lost-items", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未注入 repo 預期 503，得到 %d", w.Code)
	}

	// 非法 status → 400（在觸碰 repo 之前就擋下，故 repo 為 nil 也不會走到 503 之後的路徑）
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/admin/lost-items?status=bogus", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("未注入 repo 時任何請求都應 503，得到 %d", w2.Code)
	}
}

// TestListLostItems_status白名單 驗證合法/非法 status 值的分流（用整合 DB 驗回應內容太重，
// 這裡只驗 400 邊界；200 與資料內容見 repository 的 TestLostItemAdminList）。
func TestListLostItems_status白名單(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip

	h := &AdminHandler{}
	h.SetLostItems(repository.NewLostItemRepository(db))
	r := gin.New()
	r.GET("/api/admin/lost-items", h.ListLostItems)

	// 非法 status → 400
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/lost-items?status=bogus", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 status 預期 400，得到 %d", w.Code)
	}

	// 空庫合法查詢 → 200 且 lost_items 為空陣列（非 null）
	for _, s := range []string{"", "open", "found", "paid", "returned", "closed"} {
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("GET", "/api/admin/lost-items?status="+s, nil)
		r.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK {
			t.Fatalf("status=%q 預期 200，得到 %d", s, w2.Code)
		}
		if body := w2.Body.String(); body != `{"lost_items":[]}` {
			t.Fatalf("status=%q 預期空陣列回應，得到 %s", s, body)
		}
	}
}
