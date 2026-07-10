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

	cases := []struct {
		name                          string
		distanceM                     int
		wantFare, wantComm, wantNet   int64
	}{
		{"5km", 5000, 18500, 2775, 15725},   // 8500 + 20*5 元；15% = 2775
		{"距離0落到最低車資", 0, 8500, 1275, 7225}, // fare=8500，round(8500*0.15)=1275
		{"負距離視為0", -100, 8500, 1275, 7225},
		{"短程仍計里程", 100, 8700, 1305, 7395}, // 8500 + round(2000*100/1000)=8700
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fare, comm, net := fs.Quote(tc.distanceM)
			if fare != tc.wantFare || comm != tc.wantComm || net != tc.wantNet {
				t.Fatalf("Quote(%d) = (fare=%d, comm=%d, net=%d)，預期 (%d, %d, %d)",
					tc.distanceM, fare, comm, net, tc.wantFare, tc.wantComm, tc.wantNet)
			}
			if fare != comm+net {
				t.Fatalf("fare(%d) 應等於 commission(%d)+net(%d)", fare, comm, net)
			}
		})
	}
}

func TestQuoteMinFareFloor(t *testing.T) {
	// 最低車資高於起步價：距離 0 時 fare 應為 min_fare。
	fs := newFeeSettingsForTest(5000, 2000, 12000, 1000, 0)
	fare, comm, net := fs.Quote(0)
	if fare != 12000 {
		t.Fatalf("min_fare floor 失效：fare=%d，預期 12000", fare)
	}
	if comm != 1200 || net != 10800 {
		t.Fatalf("commission/net 錯誤：comm=%d net=%d，預期 1200/10800", comm, net)
	}
}

func TestValidateFeeSettingsRejectsOutOfRange(t *testing.T) {
	// repo 為 nil：驗證失敗會在觸及 repo 前 return，故可安全測試無效輸入。
	fs := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)

	neg := int64(-1)
	if err := fs.Update(&neg, nil, nil, nil, nil, nil); err != ErrInvalidFeeSettings {
		t.Fatalf("負起步價應被拒，got %v", err)
	}
	tooHighBps := int64(10001)
	if err := fs.Update(nil, nil, nil, &tooHighBps, nil, nil); err != ErrInvalidFeeSettings {
		t.Fatalf("手續費 bps > 10000 應被拒，got %v", err)
	}
}
