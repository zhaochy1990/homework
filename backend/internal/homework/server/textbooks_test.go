package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/database"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
)

// ---------- 入参校验（无需 DB：校验先于 currentUser） ----------

func TestCreateTextbookValidatesBody(t *testing.T) {
	h, priv := newHandler(t, nil, nil)
	tok := tokenFor(t, priv, nil)
	cases := []struct {
		name string
		body string
	}{
		{"bad_json", `{`},
		{"bad_subject", `{"subject":"电脑","name":"人教版数学"}`},
		{"missing_name", `{"subject":"数学","name":"   "}`},
		{"long_name", `{"subject":"数学","name":"` + strings.Repeat("长", 129) + `"}`},
		{"bad_unit", `{"subject":"数学","name":"人教版数学","units":[{"name":""}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(h, http.MethodPost, "/api/v1/textbooks", tok, strings.NewReader(c.body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body)
			}
			if got := errorCode(t, rec); got != "bad_request" {
				t.Fatalf("error = %q, want bad_request", got)
			}
		})
	}
}

func TestListTextbooksValidatesQuery(t *testing.T) {
	h, priv := newHandler(t, nil, nil)
	tok := tokenFor(t, priv, nil)
	cases := []struct {
		path string
	}{
		{"/api/v1/textbooks?subject=电脑"},
		{"/api/v1/textbooks?page=0"},
		{"/api/v1/textbooks?page=abc"},
		{"/api/v1/textbooks?page_size=101"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			rec := do(h, http.MethodGet, c.path, tok, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestAddTextbookUnitsValidates(t *testing.T) {
	h, priv := newHandler(t, nil, nil)
	tok := tokenFor(t, priv, nil)

	rec := do(h, http.MethodPost, "/api/v1/textbooks/abc/units", tok, strings.NewReader(`{"units":[{"name":"Unit 1"}]}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bad id status = %d, want 404", rec.Code)
	}
	rec = do(h, http.MethodPost, "/api/v1/textbooks/1/units", tok, strings.NewReader(`{"units":[]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty units status = %d, want 400", rec.Code)
	}
	rec = do(h, http.MethodPost, "/api/v1/textbooks/1/units", tok, strings.NewReader(`{"units":[{"name":""}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad unit status = %d, want 400", rec.Code)
	}
}

func TestSetClassTextbookValidates(t *testing.T) {
	h, priv := newHandler(t, nil, nil)
	tok := tokenFor(t, priv, nil)

	rec := do(h, http.MethodPut, "/api/v1/classes/1/textbooks", tok, strings.NewReader(`{"subject":"电脑","textbookId":1}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad subject status = %d, want 400", rec.Code)
	}
	rec = do(h, http.MethodPut, "/api/v1/classes/1/textbooks", tok, strings.NewReader(`{"subject":"数学","textbookId":0}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing textbookId status = %d, want 400", rec.Code)
	}
	rec = do(h, http.MethodPut, "/api/v1/classes/abc/textbooks", tok, strings.NewReader(`{"subject":"数学","textbookId":1}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bad class id status = %d, want 404", rec.Code)
	}
}

// ---------- 端到端闭环（教材-选教材-换教材） ----------

func TestTextbookFlow(t *testing.T) {
	if os.Getenv("TEST_MYSQL") == "" {
		t.Skip("set TEST_MYSQL=1 (and DB_*) to run against a real MySQL")
	}
	base, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(base.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	h, priv := newHandler(t, db, base)

	sub := fmt.Sprintf("t06-flow-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Where("stride_user_id = ?", sub).Delete(&model.User{}) })
	tok := tokenFor(t, priv, func(c *auth.Claims) { c.Subject = sub })

	rec := do(h, http.MethodGet, "/api/v1/me", tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /me status = %d, body = %s", rec.Code, rec.Body)
	}
	var me struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}

	class := model.Class{
		Name:       fmt.Sprintf("t06-class-%d", time.Now().UnixNano()),
		Visibility: "private",
		InviteCode: fmt.Sprintf("f%012d", time.Now().UnixNano()%1e12),
		CreatedBy:  me.ID,
	}
	if err := db.Create(&class).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("class_id = ?", class.ID).Delete(&model.ClassTextbook{})
		db.Where("class_id = ?", class.ID).Delete(&model.ClassMember{})
		db.Delete(&model.Class{}, class.ID)
	})
	if err := db.Create(&model.ClassMember{ClassID: class.ID, UserID: me.ID, Role: "admin"}).Error; err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("人教版数学-%d", time.Now().UnixNano())
	var tb struct {
		ID    uint64 `json:"id"`
		Units []struct {
			ID        uint64 `json:"id"`
			SortOrder int    `json:"sortOrder"`
		} `json:"units"`
	}
	body := fmt.Sprintf(`{"subject":"数学","name":"%s","grade":"三年级","term":"上册","units":[{"name":"Unit 1"}]}`, name)
	rec = do(h, http.MethodPost, "/api/v1/textbooks", tok, strings.NewReader(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /textbooks status = %d, body = %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tb); err != nil {
		t.Fatal(err)
	}
	if tb.ID == 0 || len(tb.Units) != 1 || tb.Units[0].SortOrder != 1 {
		t.Fatalf("unexpected textbook: %s", rec.Body)
	}
	t.Cleanup(func() {
		db.Where("textbook_id = ?", tb.ID).Delete(&model.TextbookUnit{})
		db.Delete(&model.Textbook{}, tb.ID)
	})

	rec = do(h, http.MethodGet, "/api/v1/textbooks?subject="+url.QueryEscape("数学")+"&keyword="+url.QueryEscape(name), tok, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), name) {
		t.Fatalf("GET /textbooks status = %d, body = %s", rec.Code, rec.Body)
	}

	rec = do(h, http.MethodPost, fmt.Sprintf("/api/v1/textbooks/%d/units", tb.ID), tok, strings.NewReader(`{"units":[{"name":"Unit 2"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST units status = %d, body = %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tb); err != nil || len(tb.Units) != 2 {
		t.Fatalf("units after append = %s", rec.Body)
	}

	rec = do(h, http.MethodPut, fmt.Sprintf("/api/v1/classes/%d/textbooks", class.ID), tok,
		strings.NewReader(fmt.Sprintf(`{"subject":"数学","textbookId":%d}`, tb.ID)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT class textbook status = %d, body = %s", rec.Code, rec.Body)
	}

	rec = do(h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d/textbooks", class.ID), tok, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), name) {
		t.Fatalf("GET class textbooks status = %d, body = %s", rec.Code, rec.Body)
	}
}
