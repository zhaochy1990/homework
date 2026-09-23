// Package media 实现 COS 直传链路（T07）：签发受限 STS 临时密钥、confirm 时 HEAD 校验、
// 以及超时未确认对象的孤儿清理。选型见 docs/research/003（方案 A：STS + cos-wx-sdk-v5）。
package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// UploadPrefix 是客户端可写的唯一 COS 前缀；STS 会话策略只覆盖该前缀下的单个对象。
const UploadPrefix = "uploads/"

// writeActions 是上传所需的最小写权限集合：不给读（GetObject）、不给删（DeleteObject）。
var writeActions = []string{
	"cos:PutObject",
	"cos:InitiateMultipartUpload",
	"cos:UploadPart",
	"cos:CompleteMultipartUpload",
	"cos:ListParts",
	"cos:AbortMultipartUpload",
}

type policyStatement struct {
	Effect   string   `json:"effect"`
	Action   []string `json:"action"`
	Resource []string `json:"resource"`
}

type policyDoc struct {
	Version   string            `json:"version"`
	Statement []policyStatement `json:"statement"`
}

// BuildWritePolicy 生成只允许写入单个 object key（且必须位于 uploads/ 前缀）的 CAM 会话策略。
// 收紧到具体对象，避免临时密钥泄漏后被用于写入其它路径。
func BuildWritePolicy(region, appID, bucket, objectKey string) (string, error) {
	if !strings.HasPrefix(objectKey, UploadPrefix) {
		return "", fmt.Errorf("media: object key 必须位于 %s 前缀", UploadPrefix)
	}
	if region == "" || appID == "" || bucket == "" {
		return "", errors.New("media: region/appid/bucket 不能为空")
	}
	resource := fmt.Sprintf("qcs::cos:%s:uid/%s:%s/%s", region, appID, bucket, objectKey)
	doc := policyDoc{
		Version:   "2.0",
		Statement: []policyStatement{{Effect: "allow", Action: writeActions, Resource: []string{resource}}},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
