package service

import "errors"

var (
	ErrForbidden = errors.New("無權限操作此訂單")
	ErrNotFound  = errors.New("找不到資源")
)
