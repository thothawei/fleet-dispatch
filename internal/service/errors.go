package service

import "errors"

var (
	ErrForbidden          = errors.New("無權限操作此訂單")
	ErrNotFound           = errors.New("找不到資源")
	ErrInvalidCredentials = errors.New("帳號或密碼錯誤")
	ErrInvalidCoords      = errors.New("無效的上車座標")
	ErrActiveRideExists   = errors.New("已有進行中的訂單")
	ErrRateLimited        = errors.New("叫車太頻繁，請稍後再試")
	ErrDriverOnTrip       = errors.New("行程進行中，無法下線")
	ErrDriverDisabled     = errors.New("帳號已停用，無法上線")
)
