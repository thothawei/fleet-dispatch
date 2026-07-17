package service

import "testing"

// newFeeSettingsForTest 直接組出快取（白箱），不需 DB，用來測純計算/驗證。
func newFeeSettingsForTest(base, perKm, minFare int64, bps int, membership int64) *FeeSettings {
	return &FeeSettings{
		baseFareCents:             base,
		perKmFareCents:            perKm,
		minFareCents:              minFare,
		commissionBps:             bps,
		monthlyMembershipFeeCents: membership,
	}
}

func TestQuote(t *testing.T) {
	// 起步 85 元、每公里 20 元、最低 85 元、手續費 15%。
	fs := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)

	// 台幣無小數：車資四捨五入到整數元、手續費無條件捨去到整數元，
	// 三者皆為 100 的倍數（整數元），且 fare == commission + net。
	cases := []struct {
		name                        string
		distanceM                   int
		wantFare, wantComm, wantNet int64
	}{
		// 8500 + 20*5 元 = 18500（整數元）；手續費 15%=2775 捨去到 2700
		{"5km", 5000, 18500, 2700, 15800},
		// fare=8500；手續費 8500*0.15=1275 捨去到 1200
		{"距離0落到最低車資", 0, 8500, 1200, 7300},
		{"負距離視為0", -100, 8500, 1200, 7300},
		// 8500 + round(2000*100/1000)=8700；手續費 1305 捨去到 1300
		{"短程仍計里程", 100, 8700, 1300, 7400},
		// 車資四捨五入：8500 + round(2000*1348/1000)=11196（NT$111.96）→ roundNtd → 11200（NT$112）；
		// 手續費 11200*0.15=1680 捨去到 1600
		{"車資四捨五入到整數元", 1348, 11200, 1600, 9600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 不指定車種（""）＝無清潔費，維持 O6 之前的計費語意。
			q := fs.Quote(tc.distanceM, "")
			fare, comm, net := q.FareCents, q.CommissionCents, q.DriverNetCents
			if fare != tc.wantFare || comm != tc.wantComm || net != tc.wantNet {
				t.Fatalf("Quote(%d) = (fare=%d, comm=%d, net=%d)，預期 (%d, %d, %d)",
					tc.distanceM, fare, comm, net, tc.wantFare, tc.wantComm, tc.wantNet)
			}
			if q.CleaningFeeCents != 0 {
				t.Fatalf("未指定車種不該有清潔費，得到 %d", q.CleaningFeeCents)
			}
			if fare != comm+net {
				t.Fatalf("fare(%d) 應等於 commission(%d)+net(%d)", fare, comm, net)
			}
			// 台幣整數元：三個金額都應為 100 的倍數。
			for label, v := range map[string]int64{"fare": fare, "commission": comm, "net": net} {
				if v%100 != 0 {
					t.Fatalf("%s=%d 不是整數元（應為 100 的倍數）", label, v)
				}
			}
		})
	}
}

func TestQuoteMinFareFloor(t *testing.T) {
	// 最低車資高於起步價：距離 0 時 fare 應為 min_fare。
	fs := newFeeSettingsForTest(5000, 2000, 12000, 1000, 0)
	q := fs.Quote(0, "")
	if q.FareCents != 12000 {
		t.Fatalf("min_fare floor 失效：fare=%d，預期 12000", q.FareCents)
	}
	if q.CommissionCents != 1200 || q.DriverNetCents != 10800 {
		t.Fatalf("commission/net 錯誤：comm=%d net=%d，預期 1200/10800", q.CommissionCents, q.DriverNetCents)
	}
}

func TestValidateFeeSettingsRejectsOutOfRange(t *testing.T) {
	// repo 為 nil：驗證失敗會在觸及 repo 前 return，故可安全測試無效輸入。
	fs := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)

	neg := int64(-1)
	if err := fs.Update(&neg, nil, nil, nil, nil, nil, nil, nil); err != ErrInvalidFeeSettings {
		t.Fatalf("負起步價應被拒，got %v", err)
	}
	tooHighBps := int64(10001)
	if err := fs.Update(nil, nil, nil, &tooHighBps, nil, nil, nil, nil); err != ErrInvalidFeeSettings {
		t.Fatalf("手續費 bps > 10000 應被拒，got %v", err)
	}
	if err := fs.Update(nil, nil, nil, nil, nil, &tooHighBps, nil, nil); err != ErrInvalidFeeSettings {
		t.Fatalf("遺失物處理費 bps > 10000 應被拒，got %v", err)
	}
	if err := fs.Update(nil, nil, nil, nil, nil, &neg, nil, nil); err != ErrInvalidFeeSettings {
		t.Fatalf("負遺失物處理費 bps 應被拒，got %v", err)
	}
	// O6：清潔費上限 30%（3000 bps）——這是乘客實際被收的錢，超過一律拒絕。
	overCap := int64(MaxPetCleaningFeeBps + 1)
	if err := fs.Update(nil, nil, nil, nil, nil, nil, &overCap, nil); err != ErrInvalidFeeSettings {
		t.Fatalf("清潔費 bps > 3000 應被拒，got %v", err)
	}
	if err := fs.Update(nil, nil, nil, nil, nil, nil, &neg, nil); err != ErrInvalidFeeSettings {
		t.Fatalf("負清潔費 bps 應被拒，got %v", err)
	}
}
