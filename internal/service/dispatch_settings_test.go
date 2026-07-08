package service

import "testing"

func TestDispatchSettings_Update驗證邊界(t *testing.T) {
	s := NewDispatchSettings(3000, 5, 20, 3, 5)

	radius := 50
	if err := s.Update(&radius, nil, nil, nil, nil); err != ErrInvalidDispatchSettings {
		t.Fatalf("過小 radius 預期 ErrInvalidDispatchSettings，得到 %v", err)
	}

	okRadius := 5000
	if err := s.Update(&okRadius, nil, nil, nil, nil); err != nil {
		t.Fatalf("合法 radius 更新失敗：%v", err)
	}
	got := s.JSON()
	if got["radius_m"] != 5000 {
		t.Fatalf("radius 未更新：%v", got)
	}
}

func TestDispatchSettings_部分更新(t *testing.T) {
	s := NewDispatchSettings(3000, 5, 20, 3, 5)
	maxDrv := 8
	if err := s.Update(nil, &maxDrv, nil, nil, nil); err != nil {
		t.Fatalf("更新 max_drivers 失敗：%v", err)
	}
	if s.JSON()["max_drivers"] != 8 {
		t.Fatal("max_drivers 未更新")
	}
	if s.JSON()["radius_m"] != 3000 {
		t.Fatal("未指定欄位不應變更")
	}
}
