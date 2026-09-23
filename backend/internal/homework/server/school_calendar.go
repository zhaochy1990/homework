package server

import (
	"net/http"
	"time"

	cal "github.com/zhaochy1990/homework/backend/internal/homework/calendar"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
)

// maxCalendarRangeDays 限制查询跨度，避免超大扫描。
const maxCalendarRangeDays = 400

type schoolCalendarItem struct {
	Date     string `json:"date"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Verified bool   `json:"verified"`
}

// getSchoolCalendar 列出范围内的法定节假日/调休日（api-contract §3.9）。
// 默认 [今天, 今天+1 年]；未核验记录也返回，但带 verified 标记。
func (s *Server) getSchoolCalendar(w http.ResponseWriter, r *http.Request) {
	from := cal.Today()
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := cal.ParseDate(v)
		if err != nil {
			httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "from 格式应为 YYYY-MM-DD")
			return
		}
		from = t
	}
	to := from.AddDate(0, 0, 365)
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := cal.ParseDate(v)
		if err != nil {
			httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "to 格式应为 YYYY-MM-DD")
			return
		}
		to = t
	}
	if to.Before(from) || to.Sub(from) > maxCalendarRangeDays*24*time.Hour {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "日期范围无效")
		return
	}

	entries, err := s.calendar.List(r.Context(), from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]schoolCalendarItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, schoolCalendarItem{
			Date:     e.Date.Format("2006-01-02"),
			Type:     e.Type,
			Name:     e.Name,
			Verified: e.Verified,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}
