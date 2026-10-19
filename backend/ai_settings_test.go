package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func aiFixture() aiConfiguration {
	app := builtinFinanceAI()
	app.Enabled = true
	app.ModelID = "vision"
	app.SystemPrompt = "使用简短中文描述"
	return aiConfiguration{Providers: []aiProvider{{ID: "deepseek", Name: "DeepSeek", Type: "openai", BaseURL: "https://api.example.com/v1/chat/completions/", Enabled: true, APIKey: "fixture-provider-secret"}}, Models: []aiModel{{ID: "vision", ProviderID: "deepseek", ModelID: "model-vision", Name: "Vision", Enabled: true, Vision: true}}, Applications: []aiApplication{app}}
}
func putAI(t *testing.T, a *App, s aiConfiguration, u User) (*httptest.ResponseRecorder, error) {
	t.Helper()
	w := httptest.NewRecorder()
	e := a.updateAISettings(w, httptest.NewRequest("PUT", "/api/settings/ai", strings.NewReader(encoded(s))), u)
	return w, e
}
func publicAI(t *testing.T, a *App) aiConfiguration {
	t.Helper()
	s, e := a.aiConfiguration()
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(aiResponse(s))
	var out aiConfiguration
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func requireAIStatus(t *testing.T, e error, code int) {
	t.Helper()
	var api *apiError
	if !errors.As(e, &api) || api.status != code {
		t.Fatalf("wanted status %d, got %v", code, e)
	}
}

func TestAISettingsPrivacyRevisionAndEndpointChange(t *testing.T) {
	a, _ := setup(t)
	admin := User{ID: "ai-admin", Role: "admin"}
	student := User{ID: "ai-student", Role: "student"}
	_, e := putAI(t, a, aiFixture(), student)
	requireAIStatus(t, e, 403)
	for _, handler := range []func(http.ResponseWriter, *http.Request, User) error{a.getAIConfiguration, a.discoverAIModels, a.testAIModel} {
		requireAIStatus(t, handler(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), student), 403)
	}
	w, e := putAI(t, a, aiFixture(), admin)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(w.Body.String(), "fixture-provider-secret") || strings.Contains(w.Body.String(), "keyCiphertext") {
		t.Fatal("secret in response")
	}
	stored, e := a.storedAISettings()
	if e != nil {
		t.Fatal(e)
	}
	cipher := stored.Providers[0].KeyCiphertext
	if cipher == "" || strings.Contains(encoded(stored), "fixture-provider-secret") {
		t.Fatal("secret not encrypted")
	}
	key, _ := a.mailSettingsKey(false)
	secret, e := decryptMailPassword(key, cipher)
	if e != nil || secret != "fixture-provider-secret" {
		t.Fatal("invalid encrypted key")
	}
	next := publicAI(t, a)
	if next.Revision != 1 || !next.Providers[0].APIKeySet || next.Providers[0].BaseURL != "https://api.example.com/v1" {
		t.Fatal(next)
	}
	next.Models[0].Name = "Renamed"
	if _, e = putAI(t, a, next, admin); e != nil {
		t.Fatal(e)
	}
	stored, _ = a.storedAISettings()
	if stored.Providers[0].KeyCiphertext != cipher {
		t.Fatal("key not preserved")
	}
	_, e = putAI(t, a, next, admin)
	requireAIStatus(t, e, 409)
	next = publicAI(t, a)
	next.Providers[0].BaseURL = "https://other.example.com/v1"
	_, e = putAI(t, a, next, admin)
	requireAIStatus(t, e, 400)
	next.Providers[0].APIKey = "new-fixture-secret"
	if _, e = putAI(t, a, next, admin); e != nil {
		t.Fatal(e)
	}
	next = publicAI(t, a)
	next.Applications[0].Enabled = false
	next.Providers[0].ClearAPIKey = true
	if _, e = putAI(t, a, next, admin); e != nil {
		t.Fatal(e)
	}
	stored, _ = a.storedAISettings()
	if stored.Providers[0].KeyCiphertext != "" {
		t.Fatal("key not cleared")
	}
}
func TestAIMigrationAndFinanceBinding(t *testing.T) {
	a, _ := setup(t)
	admin := User{ID: "migration-admin", Role: "admin"}
	e := a.updateFinanceRecognitionSettings(httptest.NewRecorder(), httptest.NewRequest("PATCH", "/", strings.NewReader(`{"enabled":true,"inputMode":"text","endpoint":"https://api.example.com/v1/chat/completions","model":"old-model","apiKey":"legacy-fixture"}`)), admin)
	if e != nil {
		t.Fatal(e)
	}
	original, _ := a.legacyFinanceRecognitionSettings()
	s, e := a.aiConfiguration()
	if e != nil {
		t.Fatal(e)
	}
	if s.Revision != 0 || len(s.Models) != 1 || s.Models[0].Vision || !s.Applications[0].Enabled || s.Applications[0].InputMode != "text" {
		t.Fatal(s)
	}
	if _, e = a.storedAISettings(); e == nil {
		t.Fatal("GET persisted migration")
	}
	if s.Providers[0].KeyCiphertext != original.KeyCiphertext {
		t.Fatal("migration lost credential")
	}
	s = publicAI(t, a)
	if _, e = putAI(t, a, s, admin); e != nil {
		t.Fatal(e)
	}
	e = a.updateFinanceRecognitionSettings(httptest.NewRecorder(), httptest.NewRequest("PATCH", "/", strings.NewReader(`{"enabled":false}`)), admin)
	requireAIStatus(t, e, 409)
	got, e := a.financeRecognitionSettings()
	if e != nil || !financeRecognitionAvailable(got) || got.Model != "old-model" || got.InputMode != "text" {
		t.Fatal(got, e)
	}
	s = publicAI(t, a)
	s.Models[0].ModelID = "replacement"
	s.Applications[0].SystemPrompt = "短标题"
	if _, e = putAI(t, a, s, admin); e != nil {
		t.Fatal(e)
	}
	resolved, e := a.resolveAIApplication("finance-invoice")
	if e != nil || resolved.Model.ModelID != "replacement" {
		t.Fatal(resolved, e)
	}
	got, _ = a.financeRecognitionSettings()
	if got.Model != "replacement" {
		t.Fatal(got)
	}
	for _, part := range []string{"provider", "model"} {
		s = publicAI(t, a)
		if part == "provider" {
			s.Providers[0].Enabled = false
		} else {
			s.Models[0].Enabled = false
		}
		if _, e = putAI(t, a, s, admin); e != nil {
			t.Fatal(e)
		}
		got, e = a.financeRecognitionSettings()
		if e != nil || financeRecognitionAvailable(got) {
			t.Fatal("disabled service remained available", got, e)
		}
	}
}
func TestAIConfigReferencesAndCapabilities(t *testing.T) {
	a, _ := setup(t)
	admin := User{ID: "validation-admin", Role: "admin"}
	if _, e := putAI(t, a, aiFixture(), admin); e != nil {
		t.Fatal(e)
	}
	cases := map[string]func(*aiConfiguration){"delete provider": func(s *aiConfiguration) { s.Providers = nil }, "delete bound model": func(s *aiConfiguration) { s.Models = nil }, "no vision": func(s *aiConfiguration) { s.Models[0].Vision = false }, "duplicate model": func(s *aiConfiguration) { s.Models = append(s.Models, s.Models[0]) }, "duplicate provider": func(s *aiConfiguration) { s.Providers = append(s.Providers, s.Providers[0]) }, "remove builtin": func(s *aiConfiguration) { s.Applications = nil }, "private endpoint": func(s *aiConfiguration) { s.Providers[0].BaseURL = "https://127.0.0.1/v1" }, "encoded path": func(s *aiConfiguration) { s.Providers[0].BaseURL = "https://api.example.com/%2e%2e/v1" }, "injected ciphertext": func(s *aiConfiguration) { s.Providers[0].KeyCiphertext = "foreign" }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := publicAI(t, a)
			change(&s)
			_, e := putAI(t, a, s, admin)
			requireAIStatus(t, e, 400)
		})
	}
	s := publicAI(t, a)
	s.Models = nil
	s.Providers = nil
	s.Applications[0].Enabled = false
	s.Applications[0].ModelID = ""
	s.Applications = append(s.Applications, aiApplication{ID: "future-helper", Name: "未来助手", Description: "仅保存配置", InputMode: "text"})
	if _, e := putAI(t, a, s, admin); e != nil {
		t.Fatal("valid unbind/remove failed", e)
	}
}
func TestAIUpstreamDiscoveryAndSyntheticTest(t *testing.T) {
	p := aiProvider{BaseURL: "https://api.example.com/v1"}
	m := aiModel{ModelID: "test-model"}
	calls := 0
	client := &http.Client{Transport: financeRecognitionRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatal("missing key")
		}
		response := ""
		if r.Method == "GET" {
			if r.URL.Path != "/v1/models" {
				t.Fatal(r.URL)
			}
			response = `{"data":[{"id":"a","name":"A","input_modalities":["text","image"]},{"id":"b"},{"id":"a"}]}`
		} else {
			if r.URL.Path != "/v1/chat/completions" {
				t.Fatal(r.URL)
			}
			var body map[string]any
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Fatal(e)
			}
			if body["model"] != "test-model" || !strings.Contains(encoded(body), "synthetic connection test") || strings.Contains(encoded(body), "image_url") {
				t.Fatal(body)
			}
			response = `{"choices":[{"message":{"content":"OK"}}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
	items, e := requestAIModels(context.Background(), client, p, "fixture-key")
	if e != nil || len(items) != 2 || !items[0].Vision || items[1].Vision {
		t.Fatal(items, e)
	}
	if _, e = requestAITest(context.Background(), client, p, m, "fixture-key"); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	client.Transport = financeRecognitionRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("fixture-key private upstream body"))}, nil
	})
	_, e = requestAIModels(context.Background(), client, p, "fixture-key")
	if e == nil || strings.Contains(e.Error(), "fixture-key") {
		t.Fatal("upstream secrets leaked", e)
	}
}

func TestAILegacyPartialAndCustomEndpointPreserved(t *testing.T) {
	for _, fixture := range []struct{ name, endpoint, model string }{
		{"custom path", "https://api.example.com/custom/invoke", "legacy"},
		{"endpoint only", "https://api.example.com/v1/chat/completions", ""},
		{"model only", "", "legacy"},
		{"key only", "", ""},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			a, _ := setup(t)
			admin := User{ID: "partial-admin", Role: "admin"}
			key, _ := a.mailSettingsKey(true)
			cipher, e := encryptMailPassword(key, "partial-fixture-key")
			if e != nil {
				t.Fatal(e)
			}
			old := financeRecognitionSettings{Endpoint: fixture.endpoint, Model: fixture.model, InputMode: "text", KeyCiphertext: cipher}
			if e = a.store.exec("INSERT INTO settings(id,payload) VALUES('finance-recognition',?)", encoded(old)); e != nil {
				t.Fatal(e)
			}
			s := publicAI(t, a)
			if len(s.Providers) != 1 || !s.Providers[0].APIKeySet {
				t.Fatal("partial credential discarded", s)
			}
			w, e := putAI(t, a, s, admin)
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(w.Body.String(), "completionEndpoint") || strings.Contains(w.Body.String(), "partial-fixture-key") {
				t.Fatal("internal metadata leaked")
			}
			stored, _ := a.storedAISettings()
			if stored.Providers[0].KeyCiphertext != cipher {
				t.Fatal("partial secret lost on save")
			}
			if fixture.name == "custom path" {
				if aiCompletionEndpoint(stored.Providers[0]) != fixture.endpoint {
					t.Fatal("custom endpoint changed")
				}
				got, _ := a.financeRecognitionSettings()
				if got.Endpoint != fixture.endpoint {
					t.Fatal("finance endpoint changed")
				}
			}
		})
	}
}
