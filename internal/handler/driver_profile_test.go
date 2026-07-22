package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/service"
)

// setupProfileRouter 與 setupVehicleRouter 同策略：格式驗證在碰 DB 前就回，故 repo 可為 nil。
func setupProfileRouter(reg *service.DriverRegistry) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewDriverHandler(nil, reg, nil, "s", 1)
	r := gin.New()
	g := r.Group("/api")
	g.Use(middleware.DriverAuth("s"))
	g.PUT("/driver/profile", h.UpdateProfile)
	return r
}

func putProfile(t *testing.T, r *gin.Engine, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/driver/profile", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestDriverProfile_授權邊界(t *testing.T) {
	r := setupProfileRouter(service.NewDriverRegistry(nil))

	if w := putProfile(t, r, "", `{"phone":"0912345678"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", w.Code)
	}
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	if w := putProfile(t, r, ctok, `{"phone":"0912345678"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("customer token 打 driver 端點預期 401，得到 %d", w.Code)
	}
}

func TestDriverProfile_參數驗證(t *testing.T) {
	r := setupProfileRouter(service.NewDriverRegistry(nil))
	dtok, _ := auth.GenerateDriverToken(7, "s", time.Hour)

	for name, body := range map[string]string{
		"缺 phone":  `{}`,
		"空字串":      `{"phone":""}`,
		"太短":       `{"phone":"1234567"}`,
		"太長":       `{"phone":"+1234567890123456"}`,
		"含英文字母":    `{"phone":"09abcdefgh"}`,
		"分隔符去掉後仍短": `{"phone":"09-12"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if w := putProfile(t, r, dtok, body); w.Code != http.StatusBadRequest {
				t.Fatalf("%s 預期 400，得到 %d：%s", name, w.Code, w.Body.String())
			}
		})
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"0912-345-678":     "0912345678",
		"(02) 2345 6789":   "0223456789",
		"  0912345678  ":   "0912345678",
		"+886 912 345 678": "+886912345678",
	}
	for in, want := range cases {
		if got := constants.NormalizePhone(in); got != want {
			t.Fatalf("NormalizePhone(%q) = %q，預期 %q", in, got, want)
		}
	}
	if constants.IsValidPhone(constants.NormalizePhone("09-12")) {
		t.Fatal("去掉分隔符後只有 4 位數不該通過")
	}
	if !constants.IsValidPhone(constants.NormalizePhone("0912-345-678")) {
		t.Fatal("正常台灣手機號應通過")
	}
}

// TestDriverProfile_改電話不重置車輛審核 這是 SetPhone 與 SetVehicle 分開的理由：
// 車輛審核（O5）通過後，司機只是更新聯絡電話卻被打回 pending，等於被鎖出派單池。
func TestDriverProfile_改電話不重置車輛審核(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip

	driverRepo := repository.NewDriverRepository(db)
	now := time.Now()
	d := &model.Driver{
		LineUserID: "D_profile_phone", Name: "電話司機", Status: constants.DriverStatusIdle,
		VehicleType: constants.VehicleTypeSedan, PlateNumber: "PHONE-01",
		VehicleReviewStatus: constants.VehicleReviewApproved,
		CreatedAt:           now, UpdatedAt: now,
	}
	if err := db.Create(d).Error; err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	r := setupProfileRouter(service.NewDriverRegistry(driverRepo))
	dtok, _ := auth.GenerateDriverToken(d.ID, "s", time.Hour)
	w := putProfile(t, r, dtok, `{"phone":"0912-345-678"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("預期 200，得到 %d：%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("回應不是合法 JSON：%v", err)
	}
	// 回應帶正規化後的號碼——乘客端會直接拿它組 tel:，分隔符留著部分機型撥不出去。
	if body["phone"] != "0912345678" {
		t.Fatalf("回應電話應為正規化後的值，得到 %v", body["phone"])
	}

	saved, err := driverRepo.FindByID(d.ID)
	if err != nil {
		t.Fatalf("重讀司機失敗：%v", err)
	}
	if saved.Phone != "0912345678" {
		t.Fatalf("DB 電話未更新：%q", saved.Phone)
	}
	if saved.VehicleReviewStatus != constants.VehicleReviewApproved {
		t.Fatalf("改電話不該重置車輛審核，實際狀態：%q", saved.VehicleReviewStatus)
	}
	if saved.VehicleType != constants.VehicleTypeSedan || saved.PlateNumber != "PHONE-01" {
		t.Fatalf("改電話不該動到車輛欄位：%+v", saved)
	}
}
