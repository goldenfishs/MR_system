package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

var bankSyncCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func fetchBankSyncArchive(ctx context.Context, settings bankSyncSettings) (string, map[string][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 85 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(address string, limit int64) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "MR-System-QuestionBankSync")
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fail(502, "连接 GitHub 失败，请稍后重试")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fail(502, fmt.Sprintf("GitHub 返回 %d，请检查公开仓库和分支", resp.StatusCode))
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return nil, fail(502, "下载题库失败")
		}
		if int64(len(data)) > limit {
			return nil, fail(400, "题库文件超出同步大小限制")
		}
		return data, nil
	}
	data, err := get("https://api.github.com/repos/"+settings.Repository+"/commits/"+url.PathEscape(settings.Branch), 2<<20)
	if err != nil {
		return "", nil, err
	}
	var result struct {
		SHA string `json:"sha"`
	}
	if json.Unmarshal(data, &result) != nil || !bankSyncCommitPattern.MatchString(result.SHA) {
		return "", nil, fail(502, "GitHub 返回无效的提交标识")
	}
	data, err = get("https://codeload.github.com/"+settings.Repository+"/tar.gz/"+result.SHA, 32<<20)
	if err != nil {
		return "", nil, err
	}
	files, err := readBankSyncTar(data)
	return result.SHA, files, err
}
func readBankSyncTar(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fail(400, "题库压缩包格式错误")
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, 128<<20))
	files := map[string][]byte{}
	root := ""
	entries := 0
	total := int64(0)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fail(400, "题库压缩包损坏或过大")
		}
		entries++
		// Git archive emits a global PAX commit header, which is metadata, not a file.
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if entries > 20000 {
			return nil, fail(400, "题库压缩包条目过多")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || path.Clean(name) != name || strings.Contains(name, "../") {
			return nil, fail(400, "题库文件路径不安全")
		}
		parts := strings.SplitN(name, "/", 2)
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			return nil, fail(400, "题库压缩包根目录不一致")
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return nil, fail(400, "题库不支持链接或特殊文件")
		}
		if len(parts) != 2 {
			return nil, fail(400, "题库目录格式错误")
		}
		if h.Size < 0 || h.Size > 8<<20 {
			return nil, fail(400, "题库单文件过大")
		}
		total += h.Size
		if total > 96<<20 {
			return nil, fail(400, "题库解压后过大")
		}
		name = parts[1]
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, exists := files[name]; exists {
			return nil, fail(400, "题库存在重复文件")
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		files[name] = body
	}
	return files, nil
}

type bankSyncManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Entrypoints   []string          `json:"entrypoints"`
	Catalogs      map[string]string `json:"catalogs"`
	Questions     map[string]string `json:"questions"`
}
type bankSyncRemoteCatalog struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Chapters      []struct {
		Questions []string `json:"questions"`
	} `json:"chapters"`
	Includes []string `json:"includes"`
}
type bankSyncRemoteQuestion struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Revision      int    `json:"revision"`
	Visibility    string `json:"visibility"`
	Task          Task   `json:"task"`
}

func (a *App) parseBankSyncArchive(files map[string][]byte, u User) ([]bankSyncCatalog, []bankSyncRecord, error) {
	read := func(name string, dest any) error {
		if name == "" || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.HasPrefix(name, "../") {
			return fail(400, "清单包含不安全路径")
		}
		data, ok := files[name]
		if !ok || len(data) > 1<<20 {
			return fail(400, "题库文件不存在或过大："+name)
		}
		if err := json.Unmarshal(data, dest); err != nil {
			return fail(400, "题库 JSON 格式错误："+name)
		}
		return nil
	}
	var manifest bankSyncManifest
	if err := read("bank.json", &manifest); err != nil {
		return nil, nil, err
	}
	if manifest.SchemaVersion != 1 || len(manifest.Catalogs) == 0 || len(manifest.Catalogs) > 50 || len(manifest.Questions) == 0 || len(manifest.Questions) > 5000 || len(manifest.Entrypoints) == 0 {
		return nil, nil, fail(400, "不支持的题库清单版本或规模")
	}
	catalogMap := map[string]bankSyncRemoteCatalog{}
	catalogKeys := []string{}
	for key, file := range manifest.Catalogs {
		var c bankSyncRemoteCatalog
		if err := read(file, &c); err != nil {
			return nil, nil, err
		}
		if !bankSyncKeyPattern.MatchString(key) || key != c.ID || c.SchemaVersion != 1 || len(c.Includes) > 50 || len(c.Chapters) > 500 || c.Name == "" || len(c.Name) > 150 {
			return nil, nil, fail(400, "题库分类格式错误")
		}
		catalogMap[key] = c
		catalogKeys = append(catalogKeys, key)
	}
	sort.Strings(catalogKeys)
	questionMap := map[string]bankSyncRemoteQuestion{}
	keys := []string{}
	for key, file := range manifest.Questions {
		var q bankSyncRemoteQuestion
		if err := read(file, &q); err != nil {
			return nil, nil, err
		}
		t := &q.Task
		if !bankSyncKeyPattern.MatchString(key) || q.ID != key || q.SchemaVersion != 1 || q.Revision < 1 || q.Visibility != "public" || t.BankVisibility != "public" || t.Category != "problem" || t.Status != "published" || t.Required || len(t.FileIDs) > 0 || len(t.Questions) > 0 || t.TeamID != "" || t.Team != "" || t.ID != "" || t.CreatedBy != "" || t.CreatedAt != "" || t.DueAt != "" || t.StartsAt != "" || t.MySubmission != nil {
			return nil, nil, fail(400, "题目包含无效属性或非公开内容："+key)
		}
		if t.MechanicalProblem != nil && len(t.MechanicalProblem.AnswerFileIDs) > 0 {
			return nil, nil, fail(400, "远程题目不能引用本站附件："+key)
		}
		if err := a.validateTaskContent(t, u); err != nil {
			return nil, nil, fmt.Errorf("题目 %s 校验失败: %w", key, err)
		}
		questionMap[key] = q
		keys = append(keys, key)
	}
	sort.Strings(keys)
	memo := map[string]map[string]bool{}
	var expand func(string, map[string]bool) (map[string]bool, error)
	expand = func(id string, visiting map[string]bool) (map[string]bool, error) {
		if cached, ok := memo[id]; ok {
			return cached, nil
		}
		c, ok := catalogMap[id]
		if !ok || visiting[id] {
			return nil, fail(400, "分类引用不存在或循环引用")
		}
		visiting[id] = true
		defer delete(visiting, id)
		out := map[string]bool{}
		for _, chapter := range c.Chapters {
			for _, key := range chapter.Questions {
				if _, ok := questionMap[key]; !ok {
					return nil, fail(400, "分类引用不存在的题目："+key)
				}
				out[key] = true
			}
		}
		for _, child := range normalizedIDs(c.Includes) {
			other, err := expand(child, visiting)
			if err != nil {
				return nil, err
			}
			for key := range other {
				out[key] = true
			}
		}
		memo[id] = out
		return out, nil
	}
	memberships := map[string][]string{}
	catalogs := []bankSyncCatalog{}
	for _, id := range catalogKeys {
		all, err := expand(id, map[string]bool{})
		if err != nil {
			return nil, nil, err
		}
		catalogs = append(catalogs, bankSyncCatalog{id, catalogMap[id].Name, len(all)})
		for key := range all {
			memberships[key] = append(memberships[key], id)
		}
	}
	for _, id := range manifest.Entrypoints {
		if _, ok := catalogMap[id]; !ok {
			return nil, nil, fail(400, "入口分类不存在")
		}
	}
	records := []bankSyncRecord{}
	for _, key := range keys {
		if len(memberships[key]) == 0 {
			return nil, nil, fail(400, "题目未归入任何分类："+key)
		}
		records = append(records, bankSyncRecord{key, questionMap[key].Task, memberships[key]})
	}
	return catalogs, records, nil
}
