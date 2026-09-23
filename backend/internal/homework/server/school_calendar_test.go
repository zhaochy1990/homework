package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
	cal "github.com/zhaochy1990/homework/backend/internal/homework/calendar"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm/clause"
)

func TestSchoolCalendarEndpoint(t *testing.T) {
	db, base := integrationDB(t)
	h, priv := newHandler(t, db, base)

	sub := fmt.Sprintf("t09-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Where("stride_user_id = ?", sub).Delete(&model.User{}) })
	tok := tokenFor(t, priv, func(c *auth.Claims) { c.Subject = sub })

	days := []string{"2027-05-01", "2027-05-02", "2027-05-03"}
	t.Cleanup(func() { db.Where("date IN ?", days).Delete(&model.SchoolCalendar{}) })
	for _, day := range days {
		dt, err := cal.ParseDate(day)
		if err != nil {
			t.Fatal(err)
		}
		row := model.SchoolCalendar{Date: dt, Type: cal.TypeHoliday, Name: "劳动节", Verified: true}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	rec := do(h, http.MethodGet, "/api/v1/school-calendar?from=2027-05-01&to=2027-05-31", tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body)
	}
	var body struct {
		Items []struct {
			Date     string `json:"date"`
			Type     string `json:"type"`
			Name     string `json:"name"`
			Verified bool   `json:"verified"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 3 || body.Items[0].Date != "2027-05-01" || !body.Items[0].Verified {
		t.Fatalf("items = %+v", body.Items)
	}

	rec = do(h, http.MethodGet, "/api/v1/school-calendar?from=bad", tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad from = %d, want 400", rec.Code)
	}
	rec = do(h, http.MethodGet, "/api/v1/school-calendar?from=2027-06-01&to=2027-05-01", tok, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reversed range = %d, want 400", rec.Code)
	}
}
