package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLLMServiceSaveListTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" 好的 "}}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	s := &LLMService{env: func(key string) string {
		if key == "KUIBU_CONFIG_DIR" {
			return dir
		}
		return ""
	}}
	const key = "sk-app-test-1234567890"
	list, err := s.Save(LLMModelInput{Name: "ds", Provider: "openai", BaseURL: srv.URL, Model: "m", APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if list.Default != "ds" || len(list.Models) != 1 {
		t.Fatalf("list = %+v", list)
	}
	data, _ := json.Marshal(list)
	if strings.Contains(string(data), key) {
		t.Fatal("返回给前端的列表不应含明文密钥")
	}
	// 编辑时不填密钥，保留原密钥仍能调通。
	if _, err := s.Save(LLMModelInput{Name: "ds", Provider: "openai", BaseURL: srv.URL, Model: "m2"}); err != nil {
		t.Fatal(err)
	}
	reply, err := s.Test("ds")
	if err != nil || reply != "好的" {
		t.Fatalf("Test = %q, %v", reply, err)
	}
	list, err = s.Remove("ds")
	if err != nil || len(list.Models) != 0 || list.Default != "" {
		t.Fatalf("Remove = %+v, %v", list, err)
	}
}
