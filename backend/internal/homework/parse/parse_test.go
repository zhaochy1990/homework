package parse_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/parse"
)

const okDraft = `{"todos":[{"subject":"数学","content":"口算天天练 P12","estimated_minutes":15},` +
	`{"subject":"语文","content":"抄写第5课生词三遍","estimated_minutes":20}],"unparsed":["明天带一把彩笔"]}`

// chatRequest 记录假 DeepSeek 收到的请求形状。
type chatRequest struct {
	Model          string  `json:"model"`
	Temperature    float64 `json:"temperature"`
	MaxTokens      int     `json:"max_tokens"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// newClient 起一个模拟 /chat/completions 的服务并构造 Client；
// respond 返回每次调用的 (status, body)，收到的请求追加进 reqs。
func newClient(t *testing.T, respond func() (int, string)) (*parse.Client, *[]chatRequest, *httptest.Server) {
	t.Helper()
	reqs := &[]chatRequest{}
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		*reqs = append(*reqs, req)
		status, body := respond()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	c := parse.New(parse.Config{
		BaseURL: server.URL, APIKey: "sk-test", Model: "deepseek-flash", Timeout: 30 * time.Second,
	})
	return c, reqs, server
}

func chatBody(content, finishReason string) string {
	resp := map[string]any{
		"choices": []map[string]any{
			{"finish_reason": finishReason, "message": map[string]any{"role": "assistant", "content": content}},
		},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 50},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func input() parse.Input {
	date, _ := time.Parse("2006-01-02", "2026-09-23") // 星期三
	return parse.Input{Date: date, RawText: "语文抄写第5课生词三遍数学口算天天练P12"}
}

func wantParseFailed(t *testing.T, err error, detail string) {
	t.Helper()
	var perr *parse.Error
	if !errors.As(err, &perr) {
		t.Fatalf("want *parse.Error, got %v", err)
	}
	if perr.Kind != parse.ErrParseFailed || perr.Detail != detail {
		t.Fatalf("kind/detail = %s/%s, want parse_failed/%s", perr.Kind, perr.Detail, detail)
	}
}

func wantUnavailable(t *testing.T, err error, detail string) {
	t.Helper()
	var perr *parse.Error
	if !errors.As(err, &perr) {
		t.Fatalf("want *parse.Error, got %v", err)
	}
	if perr.Kind != parse.ErrUnavailable || perr.Detail != detail {
		t.Fatalf("kind/detail = %s/%s, want unavailable/%s", perr.Kind, perr.Detail, detail)
	}
}

func TestParseSuccess(t *testing.T) {
	c, reqs, _ := newClient(t, func() (int, string) { return http.StatusOK, chatBody(okDraft, "stop") })

	draft, err := c.Parse(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Todos) != 2 || len(draft.Unparsed) != 1 {
		t.Fatalf("draft = %+v", draft)
	}
	if draft.Todos[0].Subject != "数学" || draft.Todos[0].Content != "口算天天练 P12" {
		t.Fatalf("todo0 = %+v", draft.Todos[0])
	}
	if draft.Todos[0].EstimatedMinutes == nil || *draft.Todos[0].EstimatedMinutes != 15 {
		t.Fatalf("todo0 minutes = %v", draft.Todos[0].EstimatedMinutes)
	}
	if draft.Unparsed[0] != "明天带一把彩笔" {
		t.Fatalf("unparsed = %v", draft.Unparsed)
	}

	// 请求形状：JSON mode、max_tokens、system/user 两条消息。
	if len(*reqs) != 1 {
		t.Fatalf("calls = %d", len(*reqs))
	}
	req := (*reqs)[0]
	if req.Model != "deepseek-flash" || req.Temperature != 0 || req.MaxTokens != 2000 ||
		req.ResponseFormat.Type != "json_object" {
		t.Fatalf("request = %+v", req)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v", req.Messages)
	}
	if !strings.Contains(req.Messages[0].Content, "json") {
		t.Fatal("system prompt must mention json (DeepSeek JSON mode requirement)")
	}
	if !strings.Contains(req.Messages[1].Content, "2026-09-23") ||
		!strings.Contains(req.Messages[1].Content, "星期三") ||
		!strings.Contains(req.Messages[1].Content, "口算天天练P12") {
		t.Fatalf("user message = %q", req.Messages[1].Content)
	}
}

func TestParseRetryUsesHigherTemperature(t *testing.T) {
	c, reqs, _ := newClient(t, func() (int, string) { return http.StatusOK, chatBody(okDraft, "stop") })

	in := input()
	in.Retry = true
	if _, err := c.Parse(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if (*reqs)[0].Temperature != 0.3 {
		t.Fatalf("retry temperature = %v, want 0.3", (*reqs)[0].Temperature)
	}
	if (*reqs)[0].Model != "deepseek-flash" {
		t.Fatalf("retry must use the same model, got %q", (*reqs)[0].Model)
	}
}

func TestParseBadJSON(t *testing.T) {
	c, _, _ := newClient(t, func() (int, string) { return http.StatusOK, chatBody("这不是 json", "stop") })
	_, err := c.Parse(context.Background(), input())
	wantParseFailed(t, err, "bad_json")
}

func TestParseSchemaViolation(t *testing.T) {
	c, _, _ := newClient(t, func() (int, string) {
		return http.StatusOK, chatBody(`{"todos":[{"content":"缺 subject"}],"unparsed":[]}`, "stop")
	})
	_, err := c.Parse(context.Background(), input())
	wantParseFailed(t, err, "schema")
}

func TestParseBothEmpty(t *testing.T) {
	c, _, _ := newClient(t, func() (int, string) {
		return http.StatusOK, chatBody(`{"todos":[],"unparsed":[]}`, "stop")
	})
	_, err := c.Parse(context.Background(), input())
	wantParseFailed(t, err, "empty")
}

func TestParseServerErrorAndRateLimit(t *testing.T) {
	c, _, _ := newClient(t, func() (int, string) {
		return http.StatusInternalServerError, `{"error":{"message":"boom"}}`
	})
	_, err := c.Parse(context.Background(), input())
	wantUnavailable(t, err, "http_5xx")

	c, _, _ = newClient(t, func() (int, string) { return http.StatusTooManyRequests, `{"error":{"message":"rate"}}` })
	_, err = c.Parse(context.Background(), input())
	wantUnavailable(t, err, "rate_limited")
}

func TestParseNetworkError(t *testing.T) {
	c, _, server := newClient(t, func() (int, string) { return http.StatusOK, chatBody(okDraft, "stop") })
	server.Close()
	_, err := c.Parse(context.Background(), input())
	wantUnavailable(t, err, "network")
}

func TestParseTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	c := parse.New(parse.Config{
		BaseURL: server.URL, APIKey: "sk", Model: "m", Timeout: 50 * time.Millisecond,
	})
	_, err := c.Parse(context.Background(), input())
	wantUnavailable(t, err, "timeout")
}

func TestParseContentFilter(t *testing.T) {
	c, _, _ := newClient(t, func() (int, string) {
		return http.StatusBadRequest, `{"error":{"code":"content_filter","message":"敏感"}}`
	})
	_, err := c.Parse(context.Background(), input())
	wantParseFailed(t, err, "content_filter")
	var perr *parse.Error
	errors.As(err, &perr)
	// 提示语要与普通解析失败不同。
	if !strings.Contains(perr.Message, "安全") {
		t.Fatalf("content filter message = %q", perr.Message)
	}
}

func TestParseEmptyReply(t *testing.T) {
	c, _, _ := newClient(t, func() (int, string) { return http.StatusOK, chatBody("", "stop") })
	_, err := c.Parse(context.Background(), input())
	wantParseFailed(t, err, "empty_reply")
	var perr *parse.Error
	errors.As(err, &perr)
	if !strings.Contains(perr.Message, "空") {
		t.Fatalf("empty reply message = %q", perr.Message)
	}
}

func TestParseTruncatedRepairsOnce(t *testing.T) {
	truncated := `{"todos":[{"subject":"数学","content":"口算天天练 P12","estimated_minutes":15},{"subject":"语`
	c, _, _ := newClient(t, func() (int, string) { return http.StatusOK, chatBody(truncated, "length") })

	draft, err := c.Parse(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Todos) != 1 || draft.Todos[0].Subject != "数学" {
		t.Fatalf("draft = %+v", draft)
	}
}

func TestParseTruncatedBeyondRepair(t *testing.T) {
	c, _, _ := newClient(t, func() (int, string) {
		return http.StatusOK, chatBody(`{"todos":[{"sub`, "length") // 连一个完整 } 都没有
	})
	_, err := c.Parse(context.Background(), input())
	wantParseFailed(t, err, "truncated")
}

// TestValidationNormalizesOK 覆盖 homework-parsing §3.4 的全部宽容规则。
func TestValidationNormalizesOK(t *testing.T) {
	long := strings.Repeat("长", 600)
	body := `{"todos":[` +
		`{"subject":"数学课","content":"猜不出科目的活","estimated_minutes":15},` + // subject 归一 → 其他
		`{"subject":"语文","content":"零时长","estimated_minutes":0},` + // → null
		`{"subject":"数学","content":"小数时长","estimated_minutes":15.5},` + // → null
		`{"subject":"数学","content":"负时长","estimated_minutes":-3},` + // → null
		`{"subject":"数学","content":"` + long + `","estimated_minutes":20}` + // 截断到 512
		`],"unparsed":[1,"明天带彩笔"]}` // 非字符串元素剔除
	c, _, _ := newClient(t, func() (int, string) { return http.StatusOK, chatBody(body, "stop") })

	draft, err := c.Parse(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Todos[0].Subject != "其他" {
		t.Fatalf("subject = %q, want 其他", draft.Todos[0].Subject)
	}
	for i, todo := range draft.Todos[1:4] { // 零/小数/负 → null
		if todo.EstimatedMinutes != nil {
			t.Fatalf("todo%d minutes = %v, want nil", i+1, *todo.EstimatedMinutes)
		}
	}
	if draft.Todos[4].EstimatedMinutes == nil || *draft.Todos[4].EstimatedMinutes != 20 {
		t.Fatalf("todo4 minutes = %v, want 20", draft.Todos[4].EstimatedMinutes)
	}
	if n := len([]rune(draft.Todos[4].Content)); n != 512 {
		t.Fatalf("long content runes = %d, want 512", n)
	}
	if len(draft.Unparsed) != 1 || draft.Unparsed[0] != "明天带彩笔" {
		t.Fatalf("unparsed = %v, want [明天带彩笔]", draft.Unparsed)
	}
}

// TestValidationStrictFailures 覆盖 §3.4 的整次失败规则。
func TestValidationStrictFailures(t *testing.T) {
	cases := map[string]string{
		"content 为空白": `{"todos":[{"subject":"数学","content":"  ","estimated_minutes":10}],"unparsed":[]}`,
		"todos 缺失":    `{"unparsed":["x"]}`,
		"todos 不是数组":  `{"todos":"all","unparsed":[]}`,
		"顶层不是对象":      `[1,2]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c, _, _ := newClient(t, func() (int, string) { return http.StatusOK, chatBody(body, "stop") })
			_, err := c.Parse(context.Background(), input())
			wantParseFailed(t, err, "schema")
		})
	}
}

func TestPromptVersionEmbedded(t *testing.T) {
	if v := parse.PromptVersion(); v != "parse_v1" {
		t.Fatalf("PromptVersion() = %q", v)
	}
}

// TestParseRealKey 是 T10 验收的手动检查项：需要真实 key（默认 SKIP）。
func TestParseRealKey(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("set DEEPSEEK_API_KEY to run against the real API")
	}
	c := parse.New(parse.Config{
		BaseURL: "https://api.deepseek.com", APIKey: key,
		Model: "deepseek-flash", Timeout: 30 * time.Second,
	})
	in := input()
	in.RawText = "今天作业：语文抄写第5课生词三遍数学口算天天练P12英语朗读课文20分钟。明天带一把彩笔"
	draft, err := c.Parse(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(draft, "", "  ")
	t.Logf("draft:\n%s", b)
	if len(draft.Todos) < 2 {
		t.Fatalf("want >=2 todos, got %+v", draft)
	}
}
