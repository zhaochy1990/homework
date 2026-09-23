// Package subject 定义全局封闭的科目枚举（ADR 0007）。
//
// 科目是连续打卡天数（Streak）的聚合维度，取值必须全局一致：班级不能自定义、
// 不能新增、不能改名，否则同一条 streak 会因名称漂移而分裂。这里是代码内唯一
// 的科目来源。
package subject

// All 按 ADR 0007 的固定顺序列出全部合法科目。
var All = []string{
	"语文", "数学", "英语", "物理", "化学", "生物", "历史",
	"地理", "道法", "科学", "体育", "艺术", "其他",
}

var set = func() map[string]struct{} {
	m := make(map[string]struct{}, len(All))
	for _, s := range All {
		m[s] = struct{}{}
	}
	return m
}()

// Valid 判断 s 是否属于全局科目枚举。
func Valid(s string) bool {
	_, ok := set[s]
	return ok
}
