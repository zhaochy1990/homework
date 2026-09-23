package calendar

import (
	_ "embed"
	"encoding/json"
)

//go:embed seed/2026.json
var seed2026 []byte

// Seed2026 返回 research/005 的 2026 草稿条目（全部 verified=false，
// 未核验前不参与业务判定）。
func Seed2026() ([]SeedEntry, error) {
	var entries []SeedEntry
	if err := json.Unmarshal(seed2026, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
