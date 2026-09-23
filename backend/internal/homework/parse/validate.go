package parse

import (
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/zhaochy1990/homework/backend/internal/homework/subject"
)

// maxContentRunes 是 todos.content 的列宽上限（data-model：todos.content 512）。
const maxContentRunes = 512

// rawTodo 保留原始 JSON 值，便于对 estimated_minutes 做宽容归一。
type rawTodo struct {
	Subject          *string         `json:"subject"`
	Content          *string         `json:"content"`
	EstimatedMinutes json.RawMessage `json:"estimated_minutes"`
}

// validate 按 homework-parsing §3.4 做 Go 侧严格校验。
// subject 归一到"其他"而不判失败（展示维度，管理员一眼能改）；
// content 不宽容（信息本身，改错孩子就做错作业）。
func validate(content string) (Draft, error) {
	if !json.Valid([]byte(content)) {
		return Draft{}, parseFailed("bad_json", msgBadFormat)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &top); err != nil {
		return Draft{}, parseFailed("schema", msgBadFormat) // 顶层不是对象
	}

	rawTodos, ok := top["todos"]
	if !ok {
		return Draft{}, parseFailed("schema", msgBadFormat)
	}
	var raws []rawTodo
	if err := json.Unmarshal(rawTodos, &raws); err != nil {
		return Draft{}, parseFailed("schema", msgBadFormat)
	}

	todos := make([]Todo, 0, len(raws))
	for _, rt := range raws {
		if rt.Subject == nil || rt.Content == nil {
			return Draft{}, parseFailed("schema", msgBadFormat)
		}
		text := strings.TrimSpace(*rt.Content)
		if text == "" || strings.TrimSpace(*rt.Subject) == "" {
			return Draft{}, parseFailed("schema", msgBadFormat)
		}
		if n := utf8.RuneCountInString(text); n > maxContentRunes {
			text = string([]rune(text)[:maxContentRunes])
		}
		todos = append(todos, Todo{
			Subject:          normalizeSubject(*rt.Subject),
			Content:          text,
			EstimatedMinutes: normalizeMinutes(rt.EstimatedMinutes),
		})
	}

	unparsed := normalizeUnparsed(top["unparsed"])

	// 两者全空 = 解析失败（research/004 §4）。
	if len(todos) == 0 && len(unparsed) == 0 {
		return Draft{}, parseFailed("empty", msgBadFormat)
	}
	return Draft{Todos: todos, Unparsed: unparsed}, nil
}

// normalizeSubject 把不在封闭枚举里的科目归一为"其他"（无害漂移，§3.4）。
func normalizeSubject(s string) string {
	if subject.Valid(s) {
		return s
	}
	return "其他"
}

// normalizeMinutes 只接受正整数；≤0、小数、字符串等一律置 null。
func normalizeMinutes(raw json.RawMessage) *int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil || f <= 0 || f != math.Trunc(f) || f > math.MaxInt32 {
		return nil
	}
	n := int(f)
	return &n
}

// normalizeUnparsed：非数组 → 空；数组内非字符串元素剔除（§3.4）。
func normalizeUnparsed(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
