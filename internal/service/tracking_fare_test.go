package service

import "testing"

func TestBillableDistanceM(t *testing.T) {
	cases := []struct {
		name         string
		trackM       int
		routeM       int
		wantDistance int
	}{
		{"軌跡長於路線＝繞路，照實計", 6000, 5000, 6000},
		{"軌跡為0＝退路用OSRM路線", 0, 5000, 5000},
		{"軌跡稀疏偏低＝退路用OSRM路線", 1200, 5000, 5000},
		{"軌跡等於路線", 5000, 5000, 5000},
		{"無路線可用（routeM<0）用軌跡", 4200, -1, 4200},
		{"無路線且軌跡0", 0, -1, 0},
		{"負軌跡視為0，無路線", -100, -1, 0},
		{"負軌跡但有路線", -100, 3000, 3000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := billableDistanceM(tc.trackM, tc.routeM); got != tc.wantDistance {
				t.Fatalf("billableDistanceM(%d, %d) = %d，預期 %d",
					tc.trackM, tc.routeM, got, tc.wantDistance)
			}
		})
	}
}
