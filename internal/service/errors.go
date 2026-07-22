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
	ErrBadRole            = errors.New("角色無效")
	ErrSelfLockout        = errors.New("不可對自己執行此操作")
	ErrLastSuperadmin     = errors.New("不可移除最後一個 superadmin")
	ErrInvalidVehicleType = errors.New("車種無效")
	ErrInvalidPlateNumber = errors.New("車牌格式錯誤")
	// ErrInvalidPhone O7：司機聯絡電話格式錯誤。乘客端會把它組成 tel: 撥出，
	// 存進髒值等於乘客在路邊撥不通。
	ErrInvalidPhone = errors.New("電話格式錯誤")
	// ErrDriverNoVehicle O3 gate：未填車輛資訊者不得接單。訊息會原樣回給司機
	// （API 走 statusForErr → 409；LINE 走 webhook 的 err.Error() 文字回覆）。
	ErrDriverNoVehicle = errors.New("請先填寫車種與車牌才能接單")
	// ErrDriverNotApproved O5 gate：填了但尚未通過審核者不得接單。與 NoVehicle 分開，
	// 讓司機知道是「等審核」而非「沒填」（App 端正常會走等待畫面，這是直打 API 的防線）。
	ErrDriverNotApproved = errors.New("車輛審核中，通過後才能接單")
	// ErrVehicleNotPending O5：只有「待審核」的司機能被審核（避免誤審已核准或沒填車輛者）。
	ErrVehicleNotPending = errors.New("該司機車輛不在待審核狀態")
	// ErrRejectNoteRequired O5：退回必須附原因（司機要知道哪裡不對）。
	ErrRejectNoteRequired = errors.New("退回車輛審核必須填寫原因")
)
