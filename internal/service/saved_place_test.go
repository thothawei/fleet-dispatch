package service

import (
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/repository"
)

// TestSavedPlaceFlow 常用地點全流程：新增自訂地點、住家／公司是覆蓋語意（不是報錯）、
// 更新、刪除，以及擁有權隔離。
func TestSavedPlaceFlow(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	svc := NewSavedPlaceService(repository.NewSavedPlaceRepository(db))

	me, err := customers.FindOrCreateByLineUserID("U_place_me", "地點乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	other, err := customers.FindOrCreateByLineUserID("U_place_other", "另一位乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	// 住家：label 留空應自動補「住家」。
	home, err := svc.Create(me.ID, SavePlaceInput{
		Kind: constants.SavedPlaceKindHome, Address: "台北市信義區市府路1號",
		Lat: 25.0330, Lng: 121.5654,
	})
	if err != nil {
		t.Fatalf("建立住家失敗：%v", err)
	}
	if home.Label != "住家" {
		t.Fatalf("住家 label 應自動補為「住家」，得到 %q", home.Label)
	}

	// **同 kind 再送一次是覆蓋，不是報錯**——UI 上那顆按鈕叫「設定住家」，
	// 乘客預期住家換成新地址，而不是被要求先去刪掉舊的。
	home2, err := svc.Create(me.ID, SavePlaceInput{
		Kind: constants.SavedPlaceKindHome, Address: "新北市板橋區縣民大道二段1號",
		Lat: 25.0143, Lng: 121.4675,
	})
	if err != nil {
		t.Fatalf("覆蓋住家失敗：%v", err)
	}
	if home2.ID != home.ID {
		t.Fatalf("覆蓋住家應沿用同一筆（id %d），卻新增了 id %d", home.ID, home2.ID)
	}
	if home2.Address != "新北市板橋區縣民大道二段1號" {
		t.Fatalf("住家地址未被覆蓋，得到 %q", home2.Address)
	}
	if home2.Point.Lat != 25.0143 {
		t.Fatalf("住家座標未被覆蓋，得到 %v", home2.Point)
	}

	// 公司是另一個插槽，不會跟住家互相覆蓋。
	work, err := svc.Create(me.ID, SavePlaceInput{
		Kind: constants.SavedPlaceKindWork, Address: "台北市內湖區瑞光路513巷",
		Lat: 25.0797, Lng: 121.5750,
	})
	if err != nil {
		t.Fatalf("建立公司失敗：%v", err)
	}
	if work.ID == home2.ID {
		t.Fatalf("公司不該覆蓋住家")
	}

	// custom 每次都是新增一筆，不覆蓋。
	gymA, err := svc.Create(me.ID, SavePlaceInput{
		Kind: constants.SavedPlaceKindCustom, Label: "健身房", Address: "台北市大安區忠孝東路四段",
		Lat: 25.0418, Lng: 121.5490,
	})
	if err != nil {
		t.Fatalf("建立自訂地點失敗：%v", err)
	}
	gymB, err := svc.Create(me.ID, SavePlaceInput{
		Kind: constants.SavedPlaceKindCustom, Label: "媽媽家", Address: "桃園市中壢區中大路300號",
		Lat: 24.9680, Lng: 121.1950,
	})
	if err != nil {
		t.Fatalf("建立第二個自訂地點失敗：%v", err)
	}
	if gymA.ID == gymB.ID {
		t.Fatalf("自訂地點應各自獨立")
	}

	// 清單排序：住家 → 公司 → 其他（新的在前）。
	list, err := svc.List(me.ID)
	if err != nil {
		t.Fatalf("讀清單失敗：%v", err)
	}
	if len(list) != 4 {
		t.Fatalf("應有 4 筆地點，得到 %d", len(list))
	}
	if list[0].Kind != constants.SavedPlaceKindHome || list[1].Kind != constants.SavedPlaceKindWork {
		t.Fatalf("清單應以住家、公司開頭，得到 %s, %s", list[0].Kind, list[1].Kind)
	}
	if list[2].ID != gymB.ID {
		t.Fatalf("自訂地點應新的在前，得到 id %d", list[2].ID)
	}

	// 更新：改名稱與座標，kind 不動。
	updated, err := svc.Update(me.ID, gymA.ID, SavePlaceInput{
		Label: "健身房（新店）", Address: "新北市新店區北新路三段",
		Lat: 24.9700, Lng: 121.5400,
	})
	if err != nil {
		t.Fatalf("更新地點失敗：%v", err)
	}
	if updated.Label != "健身房（新店）" || updated.Kind != constants.SavedPlaceKindCustom {
		t.Fatalf("更新後 label/kind 不符：%+v", updated)
	}

	// 擁有權：別人的地點既讀不到也改不到、刪不掉。
	if _, err := svc.Update(other.ID, gymA.ID, SavePlaceInput{
		Label: "偷改", Address: "任意地址", Lat: 25.0, Lng: 121.5,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("改別人的地點應回 ErrNotFound，得到 %v", err)
	}
	if err := svc.Delete(other.ID, gymA.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("刪別人的地點應回 ErrNotFound，得到 %v", err)
	}
	otherList, err := svc.List(other.ID)
	if err != nil {
		t.Fatalf("讀他人清單失敗：%v", err)
	}
	if len(otherList) != 0 {
		t.Fatalf("另一位乘客不該看到任何地點，得到 %d 筆", len(otherList))
	}

	// 刪除自己的。
	if err := svc.Delete(me.ID, gymA.ID); err != nil {
		t.Fatalf("刪除地點失敗：%v", err)
	}
	after, _ := svc.List(me.ID)
	if len(after) != 3 {
		t.Fatalf("刪除後應剩 3 筆，得到 %d", len(after))
	}
}

// TestSavedPlaceValidation 輸入驗證：kind 白名單、必填、長度、座標。
func TestSavedPlaceValidation(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	svc := NewSavedPlaceService(repository.NewSavedPlaceRepository(db))

	me, err := customers.FindOrCreateByLineUserID("U_place_valid", "驗證乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	cases := []struct {
		name string
		in   SavePlaceInput
		want error
	}{
		{"kind 不在白名單", SavePlaceInput{
			Kind: "castle", Label: "城堡", Address: "某處", Lat: 25.0, Lng: 121.5,
		}, ErrInvalidPlaceKind},
		{"custom 沒給名稱", SavePlaceInput{
			Kind: constants.SavedPlaceKindCustom, Address: "某處", Lat: 25.0, Lng: 121.5,
		}, ErrEmptyPlaceLabel},
		{"地址空白", SavePlaceInput{
			Kind: constants.SavedPlaceKindHome, Address: "   ", Lat: 25.0, Lng: 121.5,
		}, ErrEmptyPlaceAddress},
		{"名稱過長", SavePlaceInput{
			Kind: constants.SavedPlaceKindCustom, Label: string(make([]rune, 41)), Address: "某處",
			Lat: 25.0, Lng: 121.5,
		}, ErrPlaceLabelTooLong},
		{"座標無效", SavePlaceInput{
			Kind: constants.SavedPlaceKindHome, Address: "某處", Lat: 999, Lng: 121.5,
		}, ErrInvalidCoords},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(me.ID, tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("預期 %v，得到 %v", tc.want, err)
			}
		})
	}
}
