package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Provider credentials never belong in public Settings or browser responses.
type aiProvider struct {
	CompletionEndpoint string `json:"completionEndpoint,omitempty"`
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	BaseURL            string `json:"baseUrl"`
	Enabled            bool   `json:"enabled"`
	KeyCiphertext      string `json:"keyCiphertext,omitempty"`
	APIKeySet          bool   `json:"apiKeySet"`
	APIKey             string `json:"apiKey,omitempty"`
	ClearAPIKey        bool   `json:"clearApiKey,omitempty"`
}
type aiModel struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Vision     bool   `json:"vision"`
}
type aiApplication struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Enabled      bool   `json:"enabled"`
	ModelID      string `json:"modelId"`
	InputMode    string `json:"inputMode"`
	SystemPrompt string `json:"systemPrompt"`
	Builtin      bool   `json:"builtin"`
}
type aiConfiguration struct {
	Revision     int64           `json:"revision"`
	Providers    []aiProvider    `json:"providers"`
	Models       []aiModel       `json:"models"`
	Applications []aiApplication `json:"applications"`
}
type aiResolvedApplication struct {
	Provider    aiProvider
	Model       aiModel
	Application aiApplication
}

func (a *App) registerAIConfigurationRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/settings/ai", a.wrap(true, a.getAIConfiguration))
	m.HandleFunc("PUT /api/settings/ai", a.wrap(true, a.updateAISettings))
	m.HandleFunc("POST /api/settings/ai/providers/{id}/models", a.wrapWithMutationLock(true, false, a.discoverAIModels))
	m.HandleFunc("POST /api/settings/ai/models/{id}/test", a.wrapWithMutationLock(true, false, a.testAIModel))
}
func builtinFinanceAI() aiApplication {
	return aiApplication{ID: "finance-invoice", Name: "发票识别", Description: "识别报销凭证并提取金额、日期等信息，结果仍需用户确认。", InputMode: "vision", Builtin: true}
}
func (a *App) storedAISettings() (aiConfiguration, error) {
	return readOne[aiConfiguration](a.store, "SELECT payload FROM settings WHERE id='ai-providers'")
}
func (a *App) aiConfiguration() (aiConfiguration, error) {
	s, e := a.storedAISettings()
	if !errors.Is(e, sql.ErrNoRows) {
		return s, e
	}
	s = aiConfiguration{Providers: []aiProvider{}, Models: []aiModel{}, Applications: []aiApplication{builtinFinanceAI()}}
	old, e := a.legacyFinanceRecognitionSettings()
	if e != nil {
		return s, e
	}
	if old.Endpoint != "" || old.Model != "" || old.KeyCiphertext != "" {
		base := ""
		if old.Endpoint != "" {
			var err error
			base, err = normalizeAIBaseURL(old.Endpoint)
			if err != nil {
				return s, err
			}
		}
		s.Providers = append(s.Providers, aiProvider{ID: "invoice-provider", Name: "原发票识别服务", Type: "openai", BaseURL: base, Enabled: base != "", KeyCiphertext: old.KeyCiphertext, CompletionEndpoint: old.Endpoint})
		s.Applications[0].InputMode = old.InputMode
		if old.Model != "" {
			s.Models = append(s.Models, aiModel{ID: "invoice-model", ProviderID: "invoice-provider", ModelID: old.Model, Name: old.Model, Enabled: true, Vision: old.InputMode == "vision"})
			s.Applications[0].Enabled = old.Enabled
			s.Applications[0].ModelID = "invoice-model"
		}
	}
	return s, nil
}
func aiResponse(s aiConfiguration) map[string]any {
	providers := append([]aiProvider{}, s.Providers...)
	for i := range providers {
		providers[i].CompletionEndpoint = ""
		providers[i].APIKeySet = providers[i].KeyCiphertext != ""
		providers[i].KeyCiphertext = ""
		providers[i].APIKey = ""
		providers[i].ClearAPIKey = false
	}
	return map[string]any{"revision": s.Revision, "providers": providers, "models": s.Models, "applications": s.Applications, "presets": []map[string]string{{"id": "deepseek", "name": "DeepSeek", "baseUrl": "https://api.deepseek.com", "type": "openai"}, {"id": "openai", "name": "OpenAI 兼容", "baseUrl": "https://api.openai.com/v1", "type": "openai"}}}
}
func (a *App) getAIConfiguration(w http.ResponseWriter, r *http.Request, u User) error {
	if e := financeAdmin(u); e != nil {
		return e
	}
	s, e := a.aiConfiguration()
	if e != nil {
		return e
	}
	respond(w, 200, aiResponse(s))
	return nil
}

var aiIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,79}$`)

func normalizeAIBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	raw = strings.TrimSuffix(raw, "/chat/completions")
	raw = strings.TrimRight(raw, "/")
	u, e := financeRecognitionURL(raw)
	if e != nil {
		return "", fail(400, "AI 服务必须使用公网 HTTPS Base URL，不允许账号、查询参数或内网地址")
	}
	if u.RawPath != "" || strings.Contains(u.Path, "//") || strings.Contains(u.Path, "..") {
		return "", fail(400, "AI 服务地址路径无效")
	}
	return u.String(), nil
}
func (a *App) updateAISettings(w http.ResponseWriter, r *http.Request, u User) error {
	if e := financeAdmin(u); e != nil {
		return e
	}
	var in aiConfiguration
	if e := decode(r, &in); e != nil {
		return e
	}
	old, e := a.aiConfiguration()
	if e != nil {
		return e
	}
	if in.Revision != old.Revision {
		return fail(409, "AI 配置已被其他管理员修改，请刷新后重试")
	}
	if e = a.validateAISettings(&in, old); e != nil {
		return e
	}
	in.Revision++
	if e = a.store.exec("INSERT INTO settings(id,payload) VALUES('ai-providers',?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", encoded(in)); e != nil {
		return e
	}
	a.activity(u, "更新 AI 配置", "模型提供商、模型与应用绑定")
	respond(w, 200, aiResponse(in))
	return nil
}
func (a *App) validateAISettings(s *aiConfiguration, old aiConfiguration) error {
	if len(s.Providers) > 30 || len(s.Models) > 200 || len(s.Applications) > 100 {
		return fail(400, "AI 配置数量超过限制")
	}
	oldProviders := map[string]aiProvider{}
	for _, p := range old.Providers {
		oldProviders[p.ID] = p
	}
	providers := map[string]aiProvider{}
	models := map[string]aiModel{}
	apps := map[string]bool{}
	for i := range s.Providers {
		p := &s.Providers[i]
		if !aiIDPattern.MatchString(p.ID) || providers[p.ID].ID != "" || !financeText(p.Name, 100, true) || p.Type != "openai" {
			return fail(400, "提供商 ID、名称或接口类型无效，ID 不可重复")
		}
		p.BaseURL = strings.TrimSpace(p.BaseURL)
		if p.BaseURL != "" {
			base, e := normalizeAIBaseURL(p.BaseURL)
			if e != nil {
				return e
			}
			p.BaseURL = base
		} else if p.Enabled {
			return fail(400, "启用提供商前请填写 Base URL")
		}
		prev := oldProviders[p.ID]
		keyText := strings.TrimSpace(p.APIKey)
		if p.KeyCiphertext != "" || p.CompletionEndpoint != "" {
			return fail(400, "不允许提交加密密钥字段")
		}
		if prev.KeyCiphertext != "" && prev.BaseURL != p.BaseURL && keyText == "" && !p.ClearAPIKey {
			return fail(400, "更换提供商地址时必须重新填写 API Key 或清除原密钥")
		}
		p.KeyCiphertext = prev.KeyCiphertext
		if prev.BaseURL == p.BaseURL {
			p.CompletionEndpoint = prev.CompletionEndpoint
		}
		if p.ClearAPIKey {
			p.KeyCiphertext = ""
		} else if keyText != "" {
			if len(keyText) > 4096 || strings.ContainsAny(keyText, "\x00\r\n") {
				return fail(400, "API Key 格式无效")
			}
			key, e := a.mailSettingsKey(true)
			if e != nil {
				return fail(500, "无法保存 AI 密钥")
			}
			p.KeyCiphertext, e = encryptMailPassword(key, keyText)
			if e != nil {
				return fail(500, "无法加密 AI 密钥")
			}
		}
		p.APIKey = ""
		p.APIKeySet = false
		p.ClearAPIKey = false
		providers[p.ID] = *p
	}
	for _, m := range s.Models {
		if !aiIDPattern.MatchString(m.ID) || models[m.ID].ID != "" || !financeText(m.Name, 100, true) || !financeText(m.ModelID, 200, true) || providers[m.ProviderID].ID == "" || strings.ContainsAny(m.ModelID, "\r\n") {
			return fail(400, "模型 ID、名称或提供商引用无效，ID 不可重复")
		}
		models[m.ID] = m
	}
	for i := range s.Applications {
		app := &s.Applications[i]
		if !aiIDPattern.MatchString(app.ID) || apps[app.ID] || !financeText(app.Name, 100, true) || !financeText(app.Description, 1000, false) || !financeText(app.SystemPrompt, 6000, false) || (app.InputMode != "text" && app.InputMode != "vision") {
			return fail(400, "应用配置无效，ID 不可重复")
		}
		apps[app.ID] = true
		app.Builtin = app.ID == "finance-invoice"
		if app.Builtin {
			app.Name = "发票识别"
			app.SystemPrompt = ""
		}
		if app.ModelID != "" {
			m, ok := models[app.ModelID]
			if !ok {
				return fail(400, "应用绑定的模型不存在，请先重新绑定")
			}
			if app.InputMode == "vision" && !m.Vision {
				return fail(400, "图片模式需要绑定支持视觉输入的模型")
			}
			p := providers[m.ProviderID]
			if app.Enabled && p.Enabled && m.Enabled && p.KeyCiphertext == "" {
				return fail(400, "启用应用前请配置提供商 API Key")
			}
		} else if app.Enabled {
			return fail(400, "启用应用前请选择模型")
		}
	}
	if !apps["finance-invoice"] {
		return fail(400, "内置发票识别应用不能删除，可关闭应用")
	}
	if s.Providers == nil {
		s.Providers = []aiProvider{}
	}
	if s.Models == nil {
		s.Models = []aiModel{}
	}
	return nil
}
func resolveAIApplication(s aiConfiguration, id string) (aiResolvedApplication, error) {
	for _, app := range s.Applications {
		if app.ID != id {
			continue
		}
		if !app.Enabled {
			return aiResolvedApplication{}, fail(409, "AI 应用未启用")
		}
		for _, m := range s.Models {
			if m.ID != app.ModelID {
				continue
			}
			if !m.Enabled || (app.InputMode == "vision" && !m.Vision) {
				return aiResolvedApplication{}, fail(409, "AI 模型未启用或不支持所需输入")
			}
			for _, p := range s.Providers {
				if p.ID == m.ProviderID && p.Enabled && p.KeyCiphertext != "" {
					return aiResolvedApplication{p, m, app}, nil
				}
			}
		}
	}
	return aiResolvedApplication{}, fail(409, "AI 应用尚未完成模型配置")
}
func (a *App) resolveAIApplication(id string) (aiResolvedApplication, error) {
	s, e := a.aiConfiguration()
	if e != nil {
		return aiResolvedApplication{}, e
	}
	return resolveAIApplication(s, id)
}

// aiClient is shared by every internal AI application; no browser completion proxy.
func (a *App) aiClient(ctx context.Context, p aiProvider) (*http.Client, string, error) {
	endpoint, e := financeRecognitionURL(p.BaseURL)
	if e != nil {
		return nil, "", e
	}
	c, e := financeRecognitionClient(ctx, endpoint)
	if e != nil {
		return nil, "", e
	}
	key, e := a.mailSettingsKey(false)
	if e != nil {
		return nil, "", fail(503, "AI 服务密钥不可用")
	}
	secret, e := decryptMailPassword(key, p.KeyCiphertext)
	if e != nil {
		return nil, "", fail(503, "AI 服务密钥不可用")
	}
	return c, secret, nil
}
func requestAIJSON(ctx context.Context, c *http.Client, method, endpoint, key string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, e := json.Marshal(payload)
		if e != nil {
			return nil, fail(500, "AI 请求格式无效")
		}
		body = bytes.NewReader(data)
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, body)
	if e != nil {
		return nil, fail(502, "AI 请求失败")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, e := c.Do(req)
	if e != nil {
		return nil, fail(502, "AI 服务连接失败或超时")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fail(502, fmt.Sprintf("AI 服务返回 HTTP %d，请检查配置", res.StatusCode))
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return nil, fail(502, "AI 服务响应过大或读取失败")
	}
	return raw, nil
}
func requestAICompletion(ctx context.Context, c *http.Client, endpoint, key string, payload any) (string, error) {
	raw, e := requestAIJSON(ctx, c, "POST", endpoint, key, payload)
	if e != nil {
		return "", e
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", fail(502, "AI 服务返回内容无效")
	}
	return result.Choices[0].Message.Content, nil
}
func (a *App) callAIApplication(ctx context.Context, id string, userContent any, maxTokens int) (string, error) {
	resolved, e := a.resolveAIApplication(id)
	if e != nil {
		return "", e
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	client, key, e := a.aiClient(ctx, resolved.Provider)
	if e != nil {
		return "", e
	}
	defer client.CloseIdleConnections()
	if maxTokens < 1 || maxTokens > 4096 {
		return "", fail(400, "AI 输出长度无效")
	}
	payload := map[string]any{"model": resolved.Model.ModelID, "temperature": 0, "max_tokens": maxTokens, "messages": []any{map[string]any{"role": "system", "content": resolved.Application.SystemPrompt}, map[string]any{"role": "user", "content": userContent}}}
	return requestAICompletion(ctx, client, aiCompletionEndpoint(resolved.Provider), key, payload)
}

type aiDiscoveredModel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Vision bool   `json:"vision"`
}

func requestAIModels(ctx context.Context, client *http.Client, p aiProvider, key string) ([]aiDiscoveredModel, error) {
	raw, e := requestAIJSON(ctx, client, "GET", p.BaseURL+"/models", key, nil)
	if e != nil {
		return nil, e
	}
	var result struct {
		Data []struct {
			ID              string   `json:"id"`
			Name            string   `json:"name"`
			InputModalities []string `json:"input_modalities"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Data == nil {
		return nil, fail(502, "提供商未返回有效模型列表，可手动添加模型")
	}
	items := []aiDiscoveredModel{}
	seen := map[string]bool{}
	for _, m := range result.Data {
		if len(items) >= 500 {
			break
		}
		if !financeText(m.ID, 200, true) || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		item := aiDiscoveredModel{ID: m.ID, Name: m.ID}
		if financeText(m.Name, 100, true) {
			item.Name = m.Name
		}
		for _, mod := range m.InputModalities {
			if mod == "image" {
				item.Vision = true
			}
		}
		items = append(items, item)
	}
	return items, nil
}
func (a *App) discoverAIModels(w http.ResponseWriter, r *http.Request, u User) error {
	if e := financeAdmin(u); e != nil {
		return e
	}
	s, e := a.aiConfiguration()
	if e != nil {
		return e
	}
	var p aiProvider
	for _, v := range s.Providers {
		if v.ID == r.PathValue("id") {
			p = v
			break
		}
	}
	if p.ID == "" {
		return fail(404, "提供商不存在，请先保存配置")
	}
	if p.KeyCiphertext == "" {
		return fail(409, "请先保存提供商 API Key")
	}
	done, e := startFinanceRecognition(u.ID)
	if e != nil {
		return e
	}
	defer done()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	client, key, e := a.aiClient(ctx, p)
	if e != nil {
		return e
	}
	defer client.CloseIdleConnections()
	items, e := requestAIModels(ctx, client, p, key)
	if e != nil {
		return e
	}
	respond(w, 200, map[string]any{"items": items})
	return nil
}
func (a *App) testAIModel(w http.ResponseWriter, r *http.Request, u User) error {
	if e := financeAdmin(u); e != nil {
		return e
	}
	s, e := a.aiConfiguration()
	if e != nil {
		return e
	}
	var m aiModel
	var p aiProvider
	for _, v := range s.Models {
		if v.ID == r.PathValue("id") {
			m = v
			break
		}
	}
	if m.ID == "" {
		return fail(404, "模型不存在，请先保存配置")
	}
	for _, v := range s.Providers {
		if v.ID == m.ProviderID {
			p = v
			break
		}
	}
	if p.KeyCiphertext == "" {
		return fail(409, "请先保存提供商 API Key")
	}
	done, e := startFinanceRecognition(u.ID)
	if e != nil {
		return e
	}
	defer done()
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	client, key, e := a.aiClient(ctx, p)
	if e != nil {
		return e
	}
	defer client.CloseIdleConnections()
	_, e = requestAITest(ctx, client, p, m, key)
	if e != nil {
		return e
	}
	respond(w, 200, map[string]any{"ok": true, "message": "连接成功，模型可返回文本；视觉能力需以模型官方说明为准。", "latencyMs": time.Since(start).Milliseconds()})
	return nil
}
func requestAITest(ctx context.Context, c *http.Client, p aiProvider, m aiModel, key string) (string, error) {
	return requestAICompletion(ctx, c, aiCompletionEndpoint(p), key, map[string]any{"model": m.ModelID, "max_tokens": 32, "messages": []any{map[string]any{"role": "user", "content": "This is a synthetic connection test. Reply with OK."}}})
}

// A migrated legacy service may use a nonstandard completion path. Preserve it
// internally until the administrator explicitly changes the provider base URL.
func aiCompletionEndpoint(p aiProvider) string {
	if p.CompletionEndpoint != "" {
		return p.CompletionEndpoint
	}
	return p.BaseURL + "/chat/completions"
}
