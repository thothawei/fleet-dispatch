package handler

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"line-fleet-dispatch/internal/service"
)

// 後台強制取消的錯誤分類：狀態衝突要回 409，不能全丟 500。
//
// 為什麼值得一支測試釘住：這兩個錯誤是後台**每天**會遇到的正常情況
// （已上車、上一次其實已經取消成功），回 500 會讓監控把例行操作當成故障，
// 也讓 admin 前端無法分辨「伺服器壞了」與「已經生效了，重讀就好」。
func TestAdminCancelStatus_狀態衝突回409(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"已上車無法取消", service.ErrRideStarted, http.StatusConflict},
		{"狀態已變更（多半是上一次其實成功了）", service.ErrRideStateChanged, http.StatusConflict},
		// 包一層仍要成立——service 端未來若用 %w 加上下文，分類不能跟著失效
		{"包過一層的狀態衝突", fmt.Errorf("取消失敗: %w", service.ErrRideStateChanged), http.StatusConflict},
		{"找不到訂單", service.ErrNotFound, http.StatusNotFound},
		{"其餘真的是伺服器錯誤", errors.New("db connection lost"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := adminCancelStatus(tc.err); got != tc.want {
				t.Fatalf("adminCancelStatus(%v) = %d，期望 %d", tc.err, got, tc.want)
			}
		})
	}
}
