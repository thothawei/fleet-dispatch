package constants

// 取消原因（P4，2026-07-17）：隨 WS `ride.cancelled` payload 的 `cancel_reason` 送出。
// **機器可讀**——App 端據此顯示訊息並決定後續引導（例如「改用不指定車種重新叫車」），
// 不該去 parse 中文文案字串（文案會改，字串比對會無聲失效）。
//
// 目前只有 `giveUpIfUnaccepted`（逾時無人接單）這條路徑會帶 cancel_reason；
// 乘客主動取消／司機放棄等路徑不帶，App 端須容忍此欄位缺席。
const (
	// CancelReasonNoDriver 逾時無人接單（未指定車種，或指定了但就是沒人接）。
	CancelReasonNoDriver = "no_driver_available"
	// CancelReasonNoVehicleOfType 乘客指定了車種，但附近沒有該車種的司機。
	CancelReasonNoVehicleOfType = "no_vehicle_of_type"
)
