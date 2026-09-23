package server

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	cal "github.com/zhaochy1990/homework/backend/internal/homework/calendar"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/middleware"
	"github.com/zhaochy1990/homework/backend/internal/homework/parse"
	"github.com/zhaochy1990/homework/backend/internal/homework/session"
	"github.com/zhaochy1990/homework/backend/internal/homework/subject"
)

// maxRawTextRunes 限制粘贴原文长度；入库时还要过 msgSecCheck 的 2500 字分片。
const maxRawTextRunes = 4000

const (
	targetNew    = "new"
	targetAppend = "append"
)

type parseTodoResponse struct {
	Content          string `json:"content"`
	EstimatedMinutes *int   `json:"estimatedMinutes"`
}

type existingTodoResponse struct {
	ID               string `json:"id"`
	Content          string `json:"content"`
	EstimatedMinutes *int   `json:"estimatedMinutes"`
}

// parseGroupResponse 是确认页的一个科目分组（api-contract §3.7）。
// unparsed（"老师还提到"）是全局的，挂到第一个分组的 notes；todos 全空时兜底成 defaultSubject 一个组。
type parseGroupResponse struct {
	Subject       string                 `json:"subject"`
	Target        string                 `json:"target"`
	SessionID     *uint64                `json:"sessionId,omitempty"`
	ExistingTodos []existingTodoResponse `json:"existingTodos,omitempty"`
	Kind          string                 `json:"kind"`
	Date          string                 `json:"date"`
	StartDate     string                 `json:"startDate,omitempty"`
	EndDate       string                 `json:"endDate,omitempty"`
	HolidayName   string                 `json:"holidayName,omitempty"`
	Todos         []parseTodoResponse    `json:"todos"`
	Notes         string                 `json:"notes"`
}

// parseHomework 同步解析作业原文（admin，不入库；homework-parsing §1-§3）。
func (s *Server) parseHomework(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	if _, ok := s.requireAdmin(w, r, classID, u.ID); !ok {
		return
	}

	var body struct {
		Date           string `json:"date"`
		RawText        string `json:"rawText"`
		DefaultSubject string `json:"defaultSubject"`
		Retry          bool   `json:"retry"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	date, err := cal.ParseDate(body.Date)
	if err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "date 格式应为 YYYY-MM-DD")
		return
	}
	if date.After(cal.Today()) {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeFutureDateForbidden, "不允许解析未来日期的作业")
		return
	}
	rawText := strings.TrimSpace(body.RawText)
	if rawText == "" || utf8.RuneCountInString(rawText) > maxRawTextRunes {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "rawText 必填且不超过 4000 字")
		return
	}
	defaultSubject := body.DefaultSubject
	if defaultSubject != "" && !subject.Valid(defaultSubject) {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "defaultSubject 不在科目枚举内")
		return
	}

	if s.parser == nil {
		httpx.Write(w, http.StatusBadGateway, httpx.CodeLLMUnavailable, "AI 服务未配置")
		return
	}
	draft, err := s.parser.Parse(r.Context(), parse.Input{Date: date, RawText: rawText, Retry: body.Retry})
	var perr *parse.Error
	if errors.As(err, &perr) {
		if perr.Kind == parse.ErrUnavailable {
			httpx.Write(w, http.StatusBadGateway, httpx.CodeLLMUnavailable, perr.Message)
		} else {
			httpx.Write(w, http.StatusUnprocessableEntity, httpx.CodeLLMParseFailed, perr.Message)
		}
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	day, err := s.calendar.Resolve(r.Context(), date)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	groups := s.buildGroups(r, classID, date, &day, draft, defaultSubject)
	httpx.JSON(w, http.StatusOK, map[string]any{"groups": groups})
}

// buildGroups 把草稿按科目分组（保持原文顺序）、判 kind、探测已有 Session。
func (s *Server) buildGroups(r *http.Request, classID uint64, date time.Time, day *cal.DayKind, draft parse.Draft, defaultSubject string) []parseGroupResponse {
	dateStr := date.Format("2006-01-02")
	startStr := ""
	endStr := ""
	name := ""
	if day.Kind == cal.KindHoliday {
		startStr = day.StartDate.Format("2006-01-02")
		endStr = day.EndDate.Format("2006-01-02")
		name = day.HolidayName
	}

	// 按科目分组，保持首次出现顺序。
	// 模型判不出科目的"其他"条目，落进管理员当前浏览的科目（§2 defaultSubject 兜底）。
	fallback := defaultSubject
	order := []string{}
	bySubject := map[string][]parseTodoResponse{}
	for _, t := range draft.Todos {
		subj := t.Subject
		if subj == "其他" && fallback != "" {
			subj = fallback
		}
		if _, seen := bySubject[subj]; !seen {
			order = append(order, subj)
		}
		bySubject[subj] = append(bySubject[subj], parseTodoResponse{
			Content: t.Content, EstimatedMinutes: t.EstimatedMinutes,
		})
	}

	// todos 全空但 unparsed 非空：兜底成一个 defaultSubject 分组，承载"老师还提到"。
	if len(order) == 0 {
		if fallback == "" {
			fallback = "其他"
		}
		order = append(order, fallback)
		bySubject[fallback] = []parseTodoResponse{}
	}

	notes := strings.Join(draft.Unparsed, "\n")
	groups := make([]parseGroupResponse, 0, len(order))
	for i, subj := range order {
		g := parseGroupResponse{
			Subject: subj,
			Target:  targetNew,
			Kind:    day.Kind,
			Date:    dateStr,
			Todos:   bySubject[subj],
		}
		g.StartDate, g.EndDate, g.HolidayName = startStr, endStr, name
		if i == 0 {
			g.Notes = notes // unparsed 是全局的，只挂第一个分组
		}
		sess, err := s.sessions.FindForDate(r.Context(), classID, subj, date)
		if err == nil {
			g.Target = targetAppend
			g.SessionID = &sess.ID
			existing, err := s.sessions.TodosBySession(r.Context(), sess.ID)
			if err != nil {
				// 探测失败不阻塞解析返回（解析不入库），仅丢弃"已有条目"展示。
				middleware.Logger(r.Context()).Error("session todos probe failed", "err", err, "session_id", sess.ID)
				existing = nil
			}
			g.ExistingTodos = make([]existingTodoResponse, len(existing))
			for j, t := range existing {
				g.ExistingTodos[j] = existingTodoResponse{ID: t.ID, Content: t.Content, EstimatedMinutes: t.EstimatedMinutes}
			}
		} else if !errors.Is(err, session.ErrNotFound) {
			middleware.Logger(r.Context()).Error("session probe failed", "err", err, "class_id", classID, "subject", subj)
		}
		groups = append(groups, g)
	}
	return groups
}
