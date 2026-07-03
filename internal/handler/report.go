package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/repository"
)

// ReportHandler 報表 API
type ReportHandler struct {
	reports *repository.ReportRepository
}

func NewReportHandler(reports *repository.ReportRepository) *ReportHandler {
	return &ReportHandler{reports: reports}
}

// Daily GET /api/reports/daily?date=2026-07-03
func (h *ReportHandler) Daily(c *gin.Context) {
	date := c.DefaultQuery("date", "")
	if date == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "請提供 date 參數"})
		return
	}
	rows, err := h.reports.DailyDriverStats(date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"date": date, "drivers": rows})
}
