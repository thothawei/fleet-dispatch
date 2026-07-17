package service

import (
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
)

// stop 測試用的簡便建構。座標一律用合法的台北座標，除非該案就是要測座標。
func stop(seq int, kind, label string) StopInput {
	return StopInput{Seq: seq, Kind: kind, Lat: 25.03, Lng: 121.56, Address: "某處", PassengerLabel: label}
}

// 一趟合法的兩人四停：A 上 → B 上 → A 下 → B 下（同行乘客依序停靠）。
func validFourStops() []StopInput {
	return []StopInput{
		stop(1, constants.StopKindPickup, "A"),
		stop(2, constants.StopKindPickup, "B"),
		stop(3, constants.StopKindDropoff, "A"),
		stop(4, constants.StopKindDropoff, "B"),
	}
}

func TestValidateStops_合法情境(t *testing.T) {
	t.Run("空 stops＝單點訂單，直接放行", func(t *testing.T) {
		// 這是相容性的根本：既有 App／LINE 建單一律走這條。
		if err := validateStops(nil); err != nil {
			t.Fatalf("單點訂單不該被擋：%v", err)
		}
		if err := validateStops([]StopInput{}); err != nil {
			t.Fatalf("空 slice 不該被擋：%v", err)
		}
	})

	t.Run("兩人四停交錯", func(t *testing.T) {
		if err := validateStops(validFourStops()); err != nil {
			t.Fatalf("合法的兩人行程被擋：%v", err)
		}
	})

	t.Run("上限 5 位乘客 10 停", func(t *testing.T) {
		var stops []StopInput
		labels := []string{"A", "B", "C", "D", "E"}
		for i, l := range labels {
			stops = append(stops, stop(i+1, constants.StopKindPickup, l))
		}
		for i, l := range labels {
			stops = append(stops, stop(6+i, constants.StopKindDropoff, l))
		}
		if err := validateStops(stops); err != nil {
			t.Fatalf("5 位乘客 10 停應為合法上限：%v", err)
		}
	})

	t.Run("客戶端未依序送也可以", func(t *testing.T) {
		// 驗證只看 seq 的值，不看 slice 順序；落庫前會 sortStopsBySeq。
		s := validFourStops()
		s[0], s[3] = s[3], s[0]
		if err := validateStops(s); err != nil {
			t.Fatalf("順序打亂但 seq 正確，不該被擋：%v", err)
		}
	})
}

func TestValidateStops_非法情境(t *testing.T) {
	cases := []struct {
		name  string
		stops []StopInput
		want  error
	}{
		{
			// 6 位乘客 12 停：先撞停靠點上限。
			name: "超過停靠點上限",
			stops: func() []StopInput {
				var s []StopInput
				for i, l := range []string{"A", "B", "C", "D", "E", "F"} {
					s = append(s, stop(i+1, constants.StopKindPickup, l))
					s = append(s, stop(i+7, constants.StopKindDropoff, l))
				}
				return s
			}(),
			want: ErrTooManyStops,
		},
		{
			// 剛好 10 停但塞了 6 位乘客（有人只有單程）→ 先被配對規則擋下。
			name: "乘客未成對",
			stops: []StopInput{
				stop(1, constants.StopKindPickup, "A"),
				stop(2, constants.StopKindDropoff, "A"),
				stop(3, constants.StopKindPickup, "B"), // B 沒有下車點
			},
			want: ErrUnpairedStop,
		},
		{
			// 最容易寫錯、也最致命的一條：叫司機先送再接。
			name: "下車排在上車之前",
			stops: []StopInput{
				stop(1, constants.StopKindDropoff, "A"),
				stop(2, constants.StopKindPickup, "A"),
			},
			want: ErrDropoffBeforePickup,
		},
		{
			name: "同一位乘客兩個上車點",
			stops: []StopInput{
				stop(1, constants.StopKindPickup, "A"),
				stop(2, constants.StopKindPickup, "A"),
				stop(3, constants.StopKindDropoff, "A"),
			},
			want: ErrUnpairedStop,
		},
		{
			// seq ＝「第幾站」，重複的話「下一站是誰」沒有答案。
			name: "seq 重複",
			stops: []StopInput{
				stop(1, constants.StopKindPickup, "A"),
				stop(1, constants.StopKindDropoff, "A"),
			},
			want: ErrDuplicateStopSeq,
		},
		{
			name: "seq 從 0 起算",
			stops: []StopInput{
				stop(0, constants.StopKindPickup, "A"),
				stop(1, constants.StopKindDropoff, "A"),
			},
			want: ErrDuplicateStopSeq,
		},
		{
			name: "kind 非白名單",
			stops: []StopInput{
				stop(1, "waypoint", "A"),
				stop(2, constants.StopKindDropoff, "A"),
			},
			want: ErrInvalidStopKind,
		},
		{
			name: "沒有乘客標籤",
			stops: []StopInput{
				stop(1, constants.StopKindPickup, ""),
				stop(2, constants.StopKindDropoff, ""),
			},
			want: ErrMissingPassenger,
		},
		{
			name: "座標非法",
			stops: []StopInput{
				{Seq: 1, Kind: constants.StopKindPickup, Lat: 0, Lng: 0, PassengerLabel: "A"},
				stop(2, constants.StopKindDropoff, "A"),
			},
			want: ErrInvalidCoords,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStops(tc.stops)
			if !errors.Is(err, tc.want) {
				t.Fatalf("預期 %v，得到 %v", tc.want, err)
			}
		})
	}
}

// TestValidateStops_六位乘客 停靠點數在上限內、但乘客數超過 5 位。
func TestValidateStops_六位乘客(t *testing.T) {
	// 6 位乘客共 10 停：4 位完整成對（8 停）＋ 2 位只有單程（2 停）→ 先被配對擋下。
	// 要單獨測「乘客數上限」得讓所有人都成對，但那樣必然 >10 停 → 先撞 ErrTooManyStops。
	// 也就是說：5 位乘客各自上下車＝10 停，本來就是上限的兩種等價表述（N2 拍板）。
	var stops []StopInput
	for i, l := range []string{"A", "B", "C", "D", "E"} {
		stops = append(stops, stop(i+1, constants.StopKindPickup, l))
		stops = append(stops, stop(i+6, constants.StopKindDropoff, l))
	}
	if err := validateStops(stops); err != nil {
		t.Fatalf("5 位成對＝10 停應合法：%v", err)
	}
	// 再加一位 → 12 停 → 撞停靠點上限。
	stops = append(stops,
		StopInput{Seq: 11, Kind: constants.StopKindPickup, Lat: 25.03, Lng: 121.56, PassengerLabel: "F"},
		StopInput{Seq: 12, Kind: constants.StopKindDropoff, Lat: 25.03, Lng: 121.56, PassengerLabel: "F"},
	)
	if err := validateStops(stops); !errors.Is(err, ErrTooManyStops) {
		t.Fatalf("第 6 位乘客應被擋，得到 %v", err)
	}
}

func TestSortStopsBySeq與推導(t *testing.T) {
	s := []StopInput{
		stop(3, constants.StopKindDropoff, "A"),
		stop(1, constants.StopKindPickup, "A"),
		stop(4, constants.StopKindDropoff, "B"),
		stop(2, constants.StopKindPickup, "B"),
	}
	sorted := sortStopsBySeq(s)

	// 不得改動呼叫端的 slice。
	if s[0].Seq != 3 {
		t.Fatal("sortStopsBySeq 不該改動輸入")
	}
	for i, want := range []int{1, 2, 3, 4} {
		if sorted[i].Seq != want {
			t.Fatalf("排序錯誤：%+v", sorted)
		}
	}

	// 這兩個推導撐起 N3 的相容性：rides.pickup_point／dropoff_point 照樣有值，
	// 派單、導航、地圖、報表都不用改。
	first, ok := firstPickup(sorted)
	if !ok || first.Seq != 1 || first.PassengerLabel != "A" {
		t.Fatalf("第一個上車點應為 seq=1 的 A：%+v", first)
	}
	final, ok := finalDropoff(sorted)
	if !ok || final.Seq != 4 || final.PassengerLabel != "B" {
		t.Fatalf("最終目的地應為 seq 最大的 dropoff（B）：%+v", final)
	}
}
