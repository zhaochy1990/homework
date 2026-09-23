package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/parse"
	"github.com/zhaochy1990/homework/backend/internal/homework/server"
	"gorm.io/gorm/clause"
)

// fakeDeepSeek 起一个模拟 /chat/completions 的服务，返回 base URL 与收到的请求体。
func fakeDeepSeek(t *testing.T, respond func() (int, string)) (string, *[]string) {
	t.Helper()
	reqs := &[]string{}
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		b, _ := json.Marshal(req)
		*reqs = append(*reqs, string(b))
		status, body := respond()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, reqs
}

func llmBody(content string) string {
	return fmt.Sprintf(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":%q}}]}`, content)
}

const mixedDraft = `{"todos":[{"subject":"数学","content":"口算天天练 P12","estimated_minutes":15},` +
	`{"subject":"语文","content":"抄写第5课生词三遍","estimated_minutes":20}],"unparsed":["明天带一把彩笔"]}`

// decodeErrorCode 取错误响应里的稳定机器码。
func decodeErrorCode(t *testing.T, body string) string {
	t.Helper()
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return got.Error
}

// newParseEnv 装配带假 DeepSeek 的 handler 与一个管理员班。
func newParseEnv(t *testing.T, respond func() (int, string)) (*env, string, uint64) {
	t.Helper()
	db, base := integrationDB(t)
	url, _ := fakeDeepSeek(t, respond)
	h, priv := newHandler(t, db, base, server.WithParser(parse.New(parse.Config{
		BaseURL: url, APIKey: "sk-test", Model: "deepseek-flash", Timeout: 30 * time.Second,
	})))
	e := &env{t: t, db: db, h: h, priv: priv}
	t.Cleanup(e.cleanup)
	tok := e.token("admin")
	cls := e.createClass(tok, "解析班", "public", false)
	t.Cleanup(func() {
		// todos 无 class_id 列，经 session 子查询清理。
		db.Where("session_id IN (?)", db.Model(&model.HomeworkSession{}).Select("id").Where("class_id = ?", cls)).Delete(&model.Todo{})
		db.Where("class_id = ?", cls).Delete(&model.HomeworkSession{})
		db.Where("date BETWEEN ? AND ?", ptrDay("2026-04-04"), ptrDay("2026-04-06")).Delete(&model.SchoolCalendar{})
	})
	return e, tok, cls
}

func TestParseRequiresAdmin(t *testing.T) {
	e, tokA, cls := newParseEnv(t, func() (int, string) { return http.StatusOK, llmBody(mixedDraft) })
	tokB := e.token("bystander")

	path := fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls)
	body := `{"date":"2026-09-22","rawText":"口算天天练P12"}`
	// 非成员一律 404（不泄露班级存在性）。
	if rec := do(e.h, http.MethodPost, path, tokB, strings.NewReader(body)); rec.Code != http.StatusNotFound {
		t.Fatalf("non-member parse = %d, want 404, body=%s", rec.Code, rec.Body)
	}
	if rec := do(e.h, http.MethodPost, path, "", strings.NewReader(body)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token parse = %d, want 401, body=%s", rec.Code, rec.Body)
	}
	// 成员但非 admin → 403。
	do(e.h, http.MethodGet, "/api/v1/me", tokB, nil)
	var u model.User
	if err := e.db.Where("stride_user_id LIKE ?", "bystander-%").First(&u).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Create(&model.ClassMember{ClassID: cls, UserID: u.ID, Role: "member"}).Error; err != nil {
		t.Fatal(err)
	}
	if rec := do(e.h, http.MethodPost, path, tokB, strings.NewReader(body)); rec.Code != http.StatusForbidden {
		t.Fatalf("member parse = %d, want 403, body=%s", rec.Code, rec.Body)
	}
	_ = tokA
}

func TestParseRequestValidation(t *testing.T) {
	e, tok, cls := newParseEnv(t, func() (int, string) { return http.StatusOK, llmBody(mixedDraft) })
	path := fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls)

	cases := []struct {
		name string
		body string
		want int
		code string
	}{
		{"未来日期", `{"date":"2099-01-01","rawText":"口算"}`, http.StatusBadRequest, "future_date_forbidden"},
		{"坏日期格式", `{"date":"09/22","rawText":"口算"}`, http.StatusBadRequest, "bad_request"},
		{"空原文", `{"date":"2026-09-22","rawText":"   "}`, http.StatusBadRequest, "bad_request"},
		{"非法兜底科目", `{"date":"2026-09-22","rawText":"口算","defaultSubject":"奥数"}`, http.StatusBadRequest, "bad_request"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(e.h, http.MethodPost, path, tok, strings.NewReader(c.body))
			if rec.Code != c.want {
				t.Fatalf("code = %d, want %d, body=%s", rec.Code, c.want, rec.Body)
			}
			if got := decodeErrorCode(t, rec.Body.String()); got != c.code {
				t.Fatalf("error = %q, want %q", got, c.code)
			}
		})
	}
}

func TestParseSuccessWithAppendProbe(t *testing.T) {
	e, tok, cls := newParseEnv(t, func() (int, string) { return http.StatusOK, llmBody(mixedDraft) })
	db := e.db

	// 已有数学 Session（同日）与语文假期 Session（跨段）。
	daySession := model.HomeworkSession{ClassID: cls, Subject: "数学", Kind: "day", Date: ptrDay("2026-09-22"), CreatedBy: 1}
	if err := db.Create(&daySession).Error; err != nil {
		t.Fatal(err)
	}
	existingTodo := model.Todo{ID: "01PARSEEXISTINGTODO000000A", SessionID: daySession.ID, Content: "已有的数学口算", SortOrder: 0}
	if err := db.Create(&existingTodo).Error; err != nil {
		t.Fatal(err)
	}
	holidaySession := model.HomeworkSession{
		ClassID: cls, Subject: "语文", Kind: "holiday",
		StartDate: ptrDay("2026-04-04"), EndDate: ptrDay("2026-04-06"), HolidayName: "清明节", CreatedBy: 1,
	}
	if err := db.Create(&holidaySession).Error; err != nil {
		t.Fatal(err)
	}
	// 种一条 verified 清明日历（T09 已单测判定逻辑，这里只验证集成联动）。
	for _, day := range []string{"2026-04-04", "2026-04-05", "2026-04-06"} {
		row := model.SchoolCalendar{Date: *ptrDay(day), Type: "holiday", Name: "清明节", Verified: true}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	// 假期日：语文命中假期 Session（append），数学是假期新建。
	rec := do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls), tok,
		strings.NewReader(`{"date":"2026-04-05","rawText":"数学口算天天练P12语文抄写第5课生词三遍"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("parse = %d, body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		Groups []struct {
			Subject     string  `json:"subject"`
			Target      string  `json:"target"`
			SessionID   *uint64 `json:"sessionId"`
			Kind        string  `json:"kind"`
			Date        string  `json:"date"`
			StartDate   string  `json:"startDate"`
			EndDate     string  `json:"endDate"`
			HolidayName string  `json:"holidayName"`
			Todos       []struct {
				Content string `json:"content"`
			} `json:"todos"`
			Notes         string `json:"notes"`
			ExistingTodos []struct {
				ID      string `json:"id"`
				Content string `json:"content"`
			} `json:"existingTodos"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Groups) != 2 {
		t.Fatalf("groups = %+v", resp.Groups)
	}
	math, chinese := resp.Groups[0], resp.Groups[1]
	if math.Subject != "数学" || chinese.Subject != "语文" {
		t.Fatalf("group order = %s/%s", math.Subject, chinese.Subject)
	}
	if math.Kind != "holiday" || math.StartDate != "2026-04-04" || math.EndDate != "2026-04-06" || math.HolidayName != "清明节" {
		t.Fatalf("holiday kind fields = %+v", math)
	}
	if math.Target != "new" || math.SessionID != nil {
		t.Fatalf("math target = %+v", math)
	}
	if chinese.Target != "append" || chinese.SessionID == nil || *chinese.SessionID != holidaySession.ID {
		t.Fatalf("chinese target = %+v", chinese)
	}
	if len(chinese.ExistingTodos) != 0 {
		t.Fatalf("chinese existingTodos = %+v", chinese.ExistingTodos)
	}
	if math.Notes != "明天带一把彩笔" || chinese.Notes != "" {
		t.Fatalf("notes = %q / %q", math.Notes, chinese.Notes)
	}

	// 上学日：数学命中当日 Session（append，带已有条目）。
	rec = do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls), tok,
		strings.NewReader(`{"date":"2026-09-22","rawText":"数学口算天天练P12语文抄写第5课生词三遍"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("parse day = %d, body=%s", rec.Code, rec.Body)
	}
	var dayResp struct {
		Groups []struct {
			Subject     string  `json:"subject"`
			Target      string  `json:"target"`
			SessionID   *uint64 `json:"sessionId"`
			Kind        string  `json:"kind"`
			Date        string  `json:"date"`
			StartDate   string  `json:"startDate"`
			EndDate     string  `json:"endDate"`
			HolidayName string  `json:"holidayName"`
			Todos       []struct {
				Content string `json:"content"`
			} `json:"todos"`
			Notes         string `json:"notes"`
			ExistingTodos []struct {
				ID      string `json:"id"`
				Content string `json:"content"`
			} `json:"existingTodos"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dayResp); err != nil {
		t.Fatal(err)
	}
	math, chinese = dayResp.Groups[0], dayResp.Groups[1]
	if math.Kind != "day" || math.Date != "2026-09-22" || math.StartDate != "" || math.HolidayName != "" {
		t.Fatalf("day kind fields = %+v", math)
	}
	if math.Target != "append" || math.SessionID == nil || *math.SessionID != daySession.ID {
		t.Fatalf("math day target = %+v", math)
	}
	if len(math.ExistingTodos) != 1 || math.ExistingTodos[0].ID != existingTodo.ID ||
		math.ExistingTodos[0].Content != "已有的数学口算" {
		t.Fatalf("existingTodos = %+v", math.ExistingTodos)
	}
	if len(math.Todos) != 1 || math.Todos[0].Content != "口算天天练 P12" {
		t.Fatalf("math todos = %+v", math.Todos)
	}
	if chinese.Target != "new" {
		t.Fatalf("chinese day target = %+v", chinese)
	}
}

func TestParseUnparsedOnlyFallsBack(t *testing.T) {
	body := `{"todos":[],"unparsed":["明天秋游，请给孩子准备午餐"]}`
	e, tok, cls := newParseEnv(t, func() (int, string) { return http.StatusOK, llmBody(body) })

	rec := do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls), tok,
		strings.NewReader(`{"date":"2026-09-22","rawText":"各位家长好，明天秋游","defaultSubject":"其他"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("parse = %d, body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		Groups []struct {
			Subject string `json:"subject"`
			Target  string `json:"target"`
			Notes   string `json:"notes"`
			Todos   []any  `json:"todos"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Groups) != 1 || resp.Groups[0].Subject != "其他" || resp.Groups[0].Notes != "明天秋游，请给孩子准备午餐" {
		t.Fatalf("groups = %+v", resp.Groups)
	}
}

func TestParseDefaultSubjectFallback(t *testing.T) {
	// 模型判不出的"其他"条目，落进管理员当前浏览的科目。
	body := `{"todos":[{"subject":"其他","content":"订正卷子","estimated_minutes":20}],"unparsed":[]}`
	e, tok, cls := newParseEnv(t, func() (int, string) { return http.StatusOK, llmBody(body) })

	rec := do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls), tok,
		strings.NewReader(`{"date":"2026-09-22","rawText":"今天把卷子订正了","defaultSubject":"数学"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("parse = %d, body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		Groups []struct {
			Subject string `json:"subject"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Groups) != 1 || resp.Groups[0].Subject != "数学" {
		t.Fatalf("groups = %+v, want [数学]", resp.Groups)
	}
}

func TestParseLLMErrors(t *testing.T) {
	// 5xx → 502 llm_unavailable。
	e, tok, cls := newParseEnv(t, func() (int, string) { return http.StatusInternalServerError, `{"error":{"message":"boom"}}` })
	path := fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls)
	body := `{"date":"2026-09-22","rawText":"口算"}`

	rec := do(e.h, http.MethodPost, path, tok, strings.NewReader(body))
	if rec.Code != http.StatusBadGateway || decodeErrorCode(t, rec.Body.String()) != "llm_unavailable" {
		t.Fatalf("5xx = %d %s, body=%s", rec.Code, decodeErrorCode(t, rec.Body.String()), rec.Body)
	}

	// 坏 JSON → 422 llm_parse_failed。
	e2, tok2, cls2 := newParseEnv(t, func() (int, string) { return http.StatusOK, llmBody("不是 json") })
	path2 := fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls2)
	rec = do(e2.h, http.MethodPost, path2, tok2, strings.NewReader(body))
	if rec.Code != http.StatusUnprocessableEntity || decodeErrorCode(t, rec.Body.String()) != "llm_parse_failed" {
		t.Fatalf("bad json = %d %s, body=%s", rec.Code, decodeErrorCode(t, rec.Body.String()), rec.Body)
	}

	// 空结果 → 422 llm_parse_failed。
	e3, tok3, cls3 := newParseEnv(t, func() (int, string) {
		return http.StatusOK, llmBody(`{"todos":[],"unparsed":[]}`)
	})
	path3 := fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls3)
	rec = do(e3.h, http.MethodPost, path3, tok3, strings.NewReader(body))
	if rec.Code != http.StatusUnprocessableEntity || decodeErrorCode(t, rec.Body.String()) != "llm_parse_failed" {
		t.Fatalf("empty = %d %s, body=%s", rec.Code, decodeErrorCode(t, rec.Body.String()), rec.Body)
	}
}

func TestParseRetrySendsHigherTemperature(t *testing.T) {
	url, reqs := fakeDeepSeek(t, func() (int, string) { return http.StatusOK, llmBody(mixedDraft) })
	db, base := integrationDB(t)
	h, priv := newHandler(t, db, base, server.WithParser(parse.New(parse.Config{
		BaseURL: url, APIKey: "sk-test", Model: "deepseek-flash", Timeout: 30 * time.Second,
	})))
	e := &env{t: t, db: db, h: h, priv: priv}
	t.Cleanup(e.cleanup)
	tok := e.token("admin")
	cls := e.createClass(tok, "重试班", "public", false)
	t.Cleanup(func() {
		db.Where("class_id = ?", cls).Delete(&model.Todo{})
		db.Where("class_id = ?", cls).Delete(&model.HomeworkSession{})
	})

	rec := do(h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/homework/parse", cls), tok,
		strings.NewReader(`{"date":"2026-09-22","rawText":"口算","retry":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("parse = %d, body=%s", rec.Code, rec.Body)
	}
	if len(*reqs) != 1 {
		t.Fatalf("llm calls = %d", len(*reqs))
	}
	var sent struct {
		Temperature float64 `json:"temperature"`
		Model       string  `json:"model"`
	}
	if err := json.Unmarshal([]byte((*reqs)[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Temperature != 0.3 {
		t.Fatalf("retry temperature = %v, want 0.3", sent.Temperature)
	}
}

// ptrDay 把 YYYY-MM-DD 转为 UTC 零点指针。
func ptrDay(s string) *time.Time {
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &tm
}
