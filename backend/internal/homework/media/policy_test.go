package media

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildWritePolicyRestrictedToUploads(t *testing.T) {
	const key = "uploads/42/checkin_media/ab12cd.mp4"
	raw, err := BuildWritePolicy("ap-shanghai", "1250000000", "homework-1250000000", key)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version   string `json:"version"`
		Statement []struct {
			Effect   string   `json:"effect"`
			Action   []string `json:"action"`
			Resource []string `json:"resource"`
		} `json:"statement"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("policy is not valid JSON: %v", err)
	}
	if doc.Version != "2.0" || len(doc.Statement) != 1 {
		t.Fatalf("unexpected policy shape: %s", raw)
	}
	st := doc.Statement[0]
	if st.Effect != "allow" {
		t.Fatalf("effect = %q, want allow", st.Effect)
	}
	wantResource := "qcs::cos:ap-shanghai:uid/1250000000:homework-1250000000/" + key
	if len(st.Resource) != 1 || st.Resource[0] != wantResource {
		t.Fatalf("resource = %v, want [%s]", st.Resource, wantResource)
	}
	for _, action := range st.Action {
		if !strings.HasPrefix(action, "cos:") {
			t.Fatalf("unexpected action %q", action)
		}
		// 临时凭证不可读、不可删、不可列举。
		switch action {
		case "cos:GetObject", "cos:DeleteObject", "cos:GetBucket", "cos:ListMultipartUploads":
			t.Fatalf("policy must not grant %q", action)
		}
	}
}

func TestBuildWritePolicyRejectsOutsideUploads(t *testing.T) {
	for _, key := range []string{"videos/x.mp4", "/uploads/x.mp4", "uploads-evil/x.mp4"} {
		if _, err := BuildWritePolicy("ap-shanghai", "1", "b", key); err == nil {
			t.Fatalf("key %q should be rejected", key)
		}
	}
}
