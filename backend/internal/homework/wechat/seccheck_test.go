package wechat

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func sign(token, timestamp, nonce string) string {
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func TestVerifyCallbackSignature(t *testing.T) {
	token, ts, nonce := "my-token", "1700000000", "abc123"
	good := sign(token, ts, nonce)
	if !VerifyCallbackSignature(token, good, ts, nonce) {
		t.Fatal("valid signature rejected")
	}
	if VerifyCallbackSignature(token, "deadbeef", ts, nonce) {
		t.Fatal("bad signature accepted")
	}
	if VerifyCallbackSignature("", good, ts, nonce) {
		t.Fatal("empty token should fail closed")
	}
}

func TestSecCheckCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tk", "expires_in": 7200})
		case "/wxa/msg_sec_check":
			var req struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			suggest := "pass"
			if req.Content == "违规" {
				suggest = "risky"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": 0, "errmsg": "ok",
				"result": map[string]any{"suggest": suggest, "label": 100},
			})
		case "/wxa/media_check_async":
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "errmsg": "ok", "trace_id": "tr-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient("app", "secret", srv.URL, "release")
	suggest, label, err := c.MsgSecCheck(context.Background(), "违规", SceneForum, "")
	if err != nil || suggest != "risky" || label != 100 {
		t.Fatalf("MsgSecCheck = %q %d %v", suggest, label, err)
	}
	if suggest, _, err := c.MsgSecCheck(context.Background(), "正常", SceneForum, ""); err != nil || suggest != "pass" {
		t.Fatalf("MsgSecCheck pass = %q %v", suggest, err)
	}
	trace, err := c.MediaCheckAsync(context.Background(), "https://cos.example/x.jpg", MediaTypeImage, SceneForum, "")
	if err != nil || trace != "tr-1" {
		t.Fatalf("MediaCheckAsync = %q %v", trace, err)
	}
}

func TestSecCheckErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/stable_token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tk", "expires_in": 7200})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 45009, "errmsg": "api freq out of limit"})
	}))
	defer srv.Close()

	c := NewClient("app", "secret", srv.URL, "release")
	if _, _, err := c.MsgSecCheck(context.Background(), "x", SceneForum, ""); err == nil {
		t.Fatal("expected error on errcode != 0")
	}
	if _, err := c.MediaCheckAsync(context.Background(), "https://x/y", MediaTypeImage, SceneForum, ""); err == nil {
		t.Fatal("expected error on errcode != 0")
	}
}

func TestParseMediaCheckEvent(t *testing.T) {
	data := []byte(`{"MsgType":"event","Event":"wxa_media_check","trace_id":"tr-1","result":{"suggest":"pass","label":100}}`)
	ev, err := ParseMediaCheckEvent(data)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Event != "wxa_media_check" || ev.TraceID != "tr-1" || ev.Result.Suggest != "pass" {
		t.Fatalf("event = %+v", ev)
	}
}
