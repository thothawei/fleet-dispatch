package service

import (
	"errors"
	"testing"
)

func TestValidatePickupCoords(t *testing.T) {
	cases := []struct {
		name    string
		lat     float64
		lng     float64
		wantErr bool
	}{
		{"台北合法座標", 25.0330, 121.5654, false},
		{"全為零視為無效", 0, 0, true},
		{"緯度超界", 91, 121, true},
		{"經度超界", 25, 181, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePickupCoords(tc.lat, tc.lng)
			if tc.wantErr && !errors.Is(err, ErrInvalidCoords) {
				t.Fatalf("預期 ErrInvalidCoords，得到 %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("預期 nil，得到 %v", err)
			}
		})
	}
}

func TestValidateOptionalDropoffCoords(t *testing.T) {
	lat, lng := 25.08, 121.57
	t.Run("未提供座標", func(t *testing.T) {
		if err := validateOptionalDropoffCoords(nil, nil); err != nil {
			t.Fatalf("預期 nil，得到 %v", err)
		}
	})
	t.Run("成對合法座標", func(t *testing.T) {
		if err := validateOptionalDropoffCoords(&lat, &lng); err != nil {
			t.Fatalf("預期 nil，得到 %v", err)
		}
	})
	t.Run("只提供 lat", func(t *testing.T) {
		if err := validateOptionalDropoffCoords(&lat, nil); !errors.Is(err, ErrInvalidCoords) {
			t.Fatalf("預期 ErrInvalidCoords，得到 %v", err)
		}
	})
}
