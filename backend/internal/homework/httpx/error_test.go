package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorShape(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, 401, CodeUnauthorized, "缺少登录凭证")

	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	var got body
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "unauthorized" || got.Message != "缺少登录凭证" {
		t.Fatalf("unexpected body: %+v", got)
	}
}
