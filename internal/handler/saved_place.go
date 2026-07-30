package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/service"
)

// SavedPlaceHandler 乘客常用地點（住家／公司／自訂）。
type SavedPlaceHandler struct {
	places *service.SavedPlaceService
}

func NewSavedPlaceHandler(places *service.SavedPlaceService) *SavedPlaceHandler {
	return &SavedPlaceHandler{places: places}
}

// savedPlaceBody 新增／更新的請求形狀。
type savedPlaceBody struct {
	Kind    string  `json:"kind"`
	Label   string  `json:"label"`
	Address string  `json:"address"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
}

// List GET /api/customer/places（乘客 JWT）：我的常用地點（住家 → 公司 → 其他）。
func (h *SavedPlaceHandler) List(c *gin.Context) {
	places, err := h.places.List(middleware.CustomerIDFromCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"places": places})
}

// Create POST /api/customer/places（乘客 JWT）：新增常用地點。
// kind=home／work 為覆蓋語意（已存在就更新那一筆），custom 才是每次新增。
func (h *SavedPlaceHandler) Create(c *gin.Context) {
	var body savedPlaceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	place, err := h.places.Create(middleware.CustomerIDFromCtx(c), service.SavePlaceInput{
		Kind:    body.Kind,
		Label:   body.Label,
		Address: body.Address,
		Lat:     body.Lat,
		Lng:     body.Lng,
	})
	if err != nil {
		c.JSON(savedPlaceStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"place": place})
}

// Update PUT /api/customer/places/:id（乘客 JWT）：改名稱／地址／座標（kind 不可改）。
func (h *SavedPlaceHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	var body savedPlaceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	place, err := h.places.Update(middleware.CustomerIDFromCtx(c), id, service.SavePlaceInput{
		Label:   body.Label,
		Address: body.Address,
		Lat:     body.Lat,
		Lng:     body.Lng,
	})
	if err != nil {
		c.JSON(savedPlaceStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"place": place})
}

// Delete DELETE /api/customer/places/:id（乘客 JWT）。
func (h *SavedPlaceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	if err := h.places.Delete(middleware.CustomerIDFromCtx(c), id); err != nil {
		c.JSON(savedPlaceStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func savedPlaceStatusForErr(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidPlaceKind),
		errors.Is(err, service.ErrEmptyPlaceLabel),
		errors.Is(err, service.ErrPlaceLabelTooLong),
		errors.Is(err, service.ErrEmptyPlaceAddress),
		errors.Is(err, service.ErrPlaceAddressTooLong),
		errors.Is(err, service.ErrInvalidCoords):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrTooManyPlaces):
		return http.StatusConflict
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
