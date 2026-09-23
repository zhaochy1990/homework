// Package parse 实现作业解析：DeepSeek 客户端、prompt 嵌入与 Go 侧严格校验（T10）。
//
// 设计依据 docs/design/homework-parsing.md §3：
//   - 同步调用、不落库；首轮 temperature 0，管理员点重试时 0.3（同模型同 prompt）；
//   - JSON mode 只保证"是合法 JSON"，schema 靠这里严格校验；
//   - 失败分类为 ErrUnavailable（502 llm_unavailable）与 ErrParseFailed（422 llm_parse_failed），
//     不自动退避重试——重试永远由管理员驱动。
package parse

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhaochy1990/homework/backend/internal/homework/middleware"
)

// Config 是客户端装配参数；模型名一律来自配置、禁止硬编码（research/004 §1.1）。
type Config struct {
	BaseURL   string // 如 https://api.deepseek.com
	APIKey    string
	Model     string        // 默认解析模型
	HardModel string        // 难例档位，预留给后续"重试换难例模型"策略（spec 现行：重试同模型）
	Timeout   time.Duration // 单次调用超时，默认 30s
}

// ErrorKind 区分两类对外错误：502 与 422。
type ErrorKind string

const (
	// ErrUnavailable 对应 502 llm_unavailable：网络/超时/5xx/429/接口行为异常。
	ErrUnavailable ErrorKind = "unavailable"
	// ErrParseFailed 对应 422 llm_parse_failed：坏 JSON/schema 不符/空结果/安全拦截。
	ErrParseFailed ErrorKind = "parse_failed"
)

// Error 是解析失败的可分类错误；Detail 供日志，Message 是给管理员的中文提示。
type Error struct {
	Kind    ErrorKind
	Detail  string // timeout / network / http_5xx / rate_limited / http_4xx / bad_json / schema / empty / empty_reply / content_filter / truncated
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("parse: %s/%s", e.Kind, e.Detail)
}

func unavailable(detail string) *Error {
	return &Error{Kind: ErrUnavailable, Detail: detail, Message: "AI 服务暂不可用，请稍后重试"}
}

func parseFailed(detail, message string) *Error {
	return &Error{Kind: ErrParseFailed, Detail: detail, Message: message}
}

const (
	msgBadFormat     = "AI 未能按约定格式解析这段作业，请重试或手动填写"
	msgContentFilter = "原文未通过模型安全检查，请重试或手动填写"
	msgEmptyReply    = "模型返回了空结果，请重试或手动填写"
)

// Todo 是校验后的一条待办草稿。
type Todo struct {
	Subject          string
	Content          string
	EstimatedMinutes *int
}

// Draft 是一次解析的产物（未入库）。
type Draft struct {
	Todos    []Todo
	Unparsed []string
}

// Input 是一次解析请求；Retry=true 时 temperature 提到 0.3。
type Input struct {
	Date    time.Time // CST 日历日，星期由客户端算好带给 prompt
	RawText string
	Retry   bool
}

type Client struct {
	cfg    Config
	http   *http.Client
	prompt string
}

//go:embed prompts/parse_v1.md
var promptFile string

var versionRe = regexp.MustCompile(`version:\s*(\S+)`)

// PromptVersion 返回嵌入 prompt 的版本号（文件头 <!-- version: ... -->）。
func PromptVersion() string {
	if m := versionRe.FindStringSubmatch(promptFile); len(m) == 2 {
		return m[1]
	}
	return "unknown"
}

// New 构造客户端；不校验 APIKey——未配置时由调用方决定行为。
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{cfg: cfg, http: &http.Client{}, prompt: promptFile}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	MaxTokens      int           `json:"max_tokens"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Code string `json:"code"`
		Msg  string `json:"message"`
	} `json:"error"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

const maxTokens = 2000

var weekdayNames = [...]string{"日", "一", "二", "三", "四", "五", "六"}

// Parse 同步调用 DeepSeek 解析作业原文；失败不自动重试。
func (c *Client) Parse(ctx context.Context, in Input) (Draft, error) {
	temperature := 0.0
	if in.Retry {
		temperature = 0.3
	}
	weekday := "日"
	if !in.Date.IsZero() {
		weekday = weekdayNames[in.Date.Weekday()]
	}
	userMsg := fmt.Sprintf("今天是 %s 星期%s。老师发布的作业原文如下：\n\n%s",
		in.Date.Format("2006-01-02"), weekday, in.RawText)

	payload, err := json.Marshal(chatRequest{
		Model: c.cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: c.prompt},
			{Role: "user", Content: userMsg},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
		ResponseFormat: struct {
			Type string `json:"type"`
		}{Type: "json_object"},
	})
	if err != nil {
		return Draft{}, unavailable("marshal")
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Draft{}, unavailable("request_build")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		detail := "network"
		if errors.Is(err, context.DeadlineExceeded) {
			detail = "timeout"
		}
		c.log(ctx, start, in, detail, 0, "")
		return Draft{}, unavailable(detail)
	}
	defer resp.Body.Close()

	var completion chatResponse
	decodeErr := json.NewDecoder(resp.Body).Decode(&completion)

	// 内容安全拒绝 / 其他 4xx。
	if resp.StatusCode >= 400 {
		detail := "http_4xx"
		if completion.Error != nil && completion.Error.Code == "content_filter" {
			detail = "content_filter"
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			detail = "rate_limited"
		} else if resp.StatusCode >= 500 {
			detail = "http_5xx"
		}
		if detail == "content_filter" {
			c.log(ctx, start, in, detail, 0, "")
			return Draft{}, parseFailed(detail, msgContentFilter)
		}
		c.log(ctx, start, in, detail, 0, "")
		return Draft{}, unavailable(detail)
	}
	if resp.StatusCode != http.StatusOK || decodeErr != nil {
		// 响应体读到一半超时也归为 timeout，别丢掉这个信号（§3.5）。
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			c.log(ctx, start, in, "timeout", 0, "")
			return Draft{}, unavailable("timeout")
		}
		c.log(ctx, start, in, "bad_response", 0, "")
		return Draft{}, unavailable("bad_response")
	}
	if len(completion.Choices) == 0 {
		c.log(ctx, start, in, "empty_reply", usageCompletion(&completion), "")
		return Draft{}, parseFailed("empty_reply", msgEmptyReply)
	}

	content := completion.Choices[0].Message.Content
	finish := completion.Choices[0].FinishReason

	// 空回复与内容安全拦截共用"提示语不同"的 422（§3.5）。
	if strings.TrimSpace(content) == "" {
		c.log(ctx, start, in, "empty_reply", usageCompletion(&completion), "")
		return Draft{}, parseFailed("empty_reply", msgEmptyReply)
	}

	// 截断：按最后一个完整 } 截断修复一次（§3.5）。
	if finish == "length" {
		repaired := repairTruncated(content)
		if draft, verr := validate(repaired); verr == nil {
			c.log(ctx, start, in, "repaired_truncation", usageCompletion(&completion), content)
			return draft, nil
		}
		c.log(ctx, start, in, "truncated", usageCompletion(&completion), content)
		return Draft{}, parseFailed("truncated", msgBadFormat)
	}

	draft, verr := validate(content)
	if verr != nil {
		var perr *Error
		if errors.As(verr, &perr) {
			c.log(ctx, start, in, perr.Detail, usageCompletion(&completion), content)
		}
		return Draft{}, verr
	}
	c.log(ctx, start, in, "ok", usageCompletion(&completion), content)
	return draft, nil
}

// log 落结构化日志：prompt 版本、模型、耗时、失败类型、原文与响应摘要（§3.5）。
// 字段约定见 middleware.Logger。
func (c *Client) log(ctx context.Context, start time.Time, in Input, outcome string, completion int, raw string) {
	logger := middleware.Logger(ctx)
	logger.Info("llm parse",
		"llm_model", c.cfg.Model,
		"llm_prompt_version", PromptVersion(),
		"llm_latency_ms", time.Since(start).Milliseconds(),
		"llm_completion_tokens", completion,
		"outcome", outcome,
		"retry", in.Retry,
		"rawtext_summary", summarize(in.RawText, 80),
		"response_summary", summarize(raw, 200),
	)
}

func usageCompletion(c *chatResponse) int {
	if c.Usage != nil {
		return c.Usage.CompletionTokens
	}
	return 0
}

// summarize 取前 n 个 rune，供日志摘要。
func summarize(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// repairTruncated 把截断的 JSON 修到能解析：截到最后一个完整 }，再补齐未闭合的括号。
func repairTruncated(s string) string {
	i := strings.LastIndex(s, "}")
	if i < 0 {
		return s
	}
	s = s[:i+1]

	var stack []byte
	inStr, esc := false, false
	for j := 0; j < len(s); j++ {
		c := s[j]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{' || c == '[':
			stack = append(stack, c)
		case c == '}' || c == ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	for j := len(stack) - 1; j >= 0; j-- {
		if stack[j] == '{' {
			s += "}"
		} else {
			s += "]"
		}
	}
	return s
}
