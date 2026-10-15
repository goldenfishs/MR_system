package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Coordinates are fractions of the original PDF page; y denotes the top of
// the text box, while FontSize is measured in PDF points.
type CertificateField struct {
	ID       string  `json:"id"`
	Key      string  `json:"key"`
	Text     string  `json:"text,omitempty"`
	Page     int     `json:"page"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	FontSize float64 `json:"fontSize"`
	Align    string  `json:"align"`
	Color    string  `json:"color"`
}

type CertificatePage struct {
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	PreviewFileID string  `json:"previewFileId"`
}

type CertificateTemplate struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	SourceFileID string             `json:"sourceFileId"`
	PDFFileID    string             `json:"pdfFileId"`
	Pages        []CertificatePage  `json:"pages"`
	Fields       []CertificateField `json:"fields"`
	CreatedAt    string             `json:"createdAt"`
	UpdatedAt    string             `json:"updatedAt"`
	CreatedBy    string             `json:"createdBy"`
}

// The renderer must write exclusively below OutputDir. Normalize imports an
// untrusted source into a static PDF plus one PNG per page. Otherwise Fields
// are overlaid on that normalized PDF; PreviewPath is the selected page PNG.
type CertificateRenderRequest struct {
	SourcePath  string             `json:"sourcePath"`
	SourceName  string             `json:"sourceName"`
	OutputDir   string             `json:"outputDir"`
	Fields      []CertificateField `json:"fields"`
	Values      map[string]string  `json:"values"`
	PreviewPage int                `json:"previewPage"`
	Normalize   bool               `json:"normalize"`
}

type CertificateRenderedPage struct {
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	PreviewPath string  `json:"previewPath"`
}

type CertificateRenderResult struct {
	PDFPath     string                    `json:"pdfPath"`
	Pages       []CertificateRenderedPage `json:"pages"`
	PreviewPath string                    `json:"previewPath"`
}

type CertificateRenderer func(context.Context, CertificateRenderRequest) (CertificateRenderResult, error)

type certificateBatch struct {
	ID          string   `json:"batchId"`
	RequestHash string   `json:"requestHash"`
	Items       []Record `json:"items"`
}

type certificateBatchInput struct {
	TemplateID      string   `json:"templateId"`
	TemplateVersion string   `json:"templateVersion"`
	UserIDs         []string `json:"userIds"`
	Title           string   `json:"title"`
	Kind            string   `json:"kind"`
	Description     string   `json:"description"`
	Direction       string   `json:"direction"`
	IssuedAt        string   `json:"issuedAt"`
	RequestID       string   `json:"requestId"`
}

func (a *App) registerCertificateRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/certificate-templates", a.wrap(true, a.listCertificateTemplates))
	// Rendering runs outside the global mutation lock. The short persistence
	// phase reacquires it and rechecks the current user's scope before commit.
	m.HandleFunc("POST /api/certificate-templates", a.wrapWithMutationLock(true, false, a.createCertificateTemplate))
	m.HandleFunc("PATCH /api/certificate-templates/{id}", a.wrap(true, a.updateCertificateTemplate))
	m.HandleFunc("POST /api/certificate-templates/{id}/preview", a.wrapWithMutationLock(true, false, a.previewCertificateTemplate))
	m.HandleFunc("POST /api/certificate-batches", a.wrapWithMutationLock(true, false, a.issueCertificateBatch))
}

func (a *App) certificateTemplate(u User, id string) (CertificateTemplate, error) {
	var item CertificateTemplate
	if !hasAnyPermission(u, "records") {
		return item, fail(403, "没有荣誉发放权限")
	}
	item, err := readOne[CertificateTemplate](a.store, "SELECT payload FROM certificate_templates WHERE id=?", id)
	if err != nil {
		return item, err
	}
	if u.Role != "admin" && item.CreatedBy != u.ID {
		return item, fail(403, "只能使用自己创建的证书模板")
	}
	return item, nil
}

func (a *App) listCertificateTemplates(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "records") {
		return fail(403, "没有荣誉发放权限")
	}
	items, err := readAll[CertificateTemplate](a.store, "SELECT payload FROM certificate_templates ORDER BY id DESC")
	if err != nil {
		return err
	}
	visible := []CertificateTemplate{}
	for _, item := range items {
		if u.Role == "admin" || item.CreatedBy == u.ID {
			visible = append(visible, item)
		}
	}
	respond(w, 200, map[string]any{"items": visible})
	return nil
}

func validCertificateName(name string) bool {
	return name != "" && len(name) <= 200 && !strings.ContainsAny(name, "\r\n\x00")
}

func validateCertificateFields(fields []CertificateField, pages []CertificatePage) error {
	if len(fields) > 40 {
		return fail(400, "每份证书最多添加 40 个填写区域")
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if f.ID == "" || len(f.ID) > 80 || seen[f.ID] || !slices.Contains([]string{"name", "direction", "major", "className", "studentNumber", "date", "text"}, f.Key) || f.Page < 0 || f.Page >= len(pages) {
			return fail(400, "证书填写区域或所在页无效")
		}
		seen[f.ID] = true
		for _, value := range []float64{f.X, f.Y, f.Width, f.FontSize} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fail(400, "填写区域位置或字号无效")
			}
		}
		if f.X < 0 || f.Y < 0 || f.Width <= 0 || f.X+f.Width > 1.000001 || f.Y >= 1 || f.FontSize < 8 || f.FontSize > 96 || f.Y*pages[f.Page].Height+f.FontSize > pages[f.Page].Height {
			return fail(400, "填写区域须位于证书内，字号须为 8–96 pt")
		}
		if !slices.Contains([]string{"left", "center", "right"}, f.Align) || len(f.Color) != 7 || !strings.HasPrefix(f.Color, "#") || strings.Trim(f.Color[1:], "0123456789abcdefABCDEF") != "" {
			return fail(400, "请选择有效的文字对齐方式和颜色")
		}
		if len(f.Text) > 1000 || strings.ContainsAny(f.Text, "\x00\r") || f.Key == "text" && strings.TrimSpace(f.Text) == "" {
			return fail(400, "自定义文字不能为空或超过 1000 字节")
		}
	}
	return nil
}

func readCertificateOutput(dir, path, format string) ([]byte, error) {
	// Resolve parent symlinks as well as rejecting symlink leaf files below.
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	realRel, err := filepath.Rel(realDir, realPath)
	if err != nil || filepath.IsAbs(realRel) || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("certificate renderer output resolves outside its output directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxAssetSize {
		return nil, fail(413, "生成的证书文件为空或超过 25 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxAssetSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAssetSize || format == "pdf" && !bytes.HasPrefix(data, []byte("%PDF-")) || format == "png" && !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return nil, fail(400, "证书转换结果无效，请更换模板后重试")
	}
	return data, nil
}

func certificateAsset(owner, name, format string, data []byte) FileAsset {
	mimeType := "application/pdf"
	if format == "png" {
		mimeType = "image/png"
	}
	return FileAsset{ID: randomID(), OwnerID: owner, Name: name + "." + format, MIME: mimeType, Size: int64(len(data)), CreatedAt: now(), CleanImage: format == "png"}
}

func (a *App) createCertificateTemplate(w http.ResponseWriter, r *http.Request, u User) error {
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if !hasAnyPermission(u, "records") {
		return fail(403, "没有荣誉发放权限")
	}
	var in struct {
		Name         string `json:"name"`
		SourceFileID string `json:"sourceFileId"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if !validCertificateName(in.Name) {
		return fail(400, "请填写不超过 200 字节的模板名称")
	}
	if err := requireOwnedFiles(a.store, u, []string{in.SourceFileID}); err != nil {
		return err
	}
	file, source, err := a.openAsset(in.SourceFileID)
	if err != nil {
		return err
	}
	sourcePath := file.Name()
	file.Close()
	if !slices.Contains([]string{".pdf", ".png", ".jpg", ".jpeg", ".docx"}, strings.ToLower(filepath.Ext(source.Name))) {
		return fail(400, "证书模板支持 PDF、PNG、JPEG 和 Word DOCX 文件")
	}
	dir, err := os.MkdirTemp("", "mr-certificate-import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	result, err := a.config.CertificateRenderer(r.Context(), CertificateRenderRequest{SourcePath: sourcePath, SourceName: source.Name, OutputDir: dir, Normalize: true})
	if err != nil {
		return err
	}
	if len(result.Pages) == 0 || len(result.Pages) > 5 {
		return fail(400, "证书模板须包含 1–5 页")
	}
	pdf, err := readCertificateOutput(dir, result.PDFPath, "pdf")
	if err != nil {
		return err
	}
	item := CertificateTemplate{ID: randomID(), Name: in.Name, SourceFileID: source.ID, Fields: []CertificateField{}, Pages: []CertificatePage{}, CreatedBy: u.ID, CreatedAt: now(), UpdatedAt: now()}
	assets := []FileAsset{certificateAsset(u.ID, "证书模板", "pdf", pdf)}
	data := [][]byte{pdf}
	item.PDFFileID = assets[0].ID
	for i, page := range result.Pages {
		if page.Width < 1 || page.Height < 1 || page.Width > 2000 || page.Height > 2000 || math.IsNaN(page.Width) || math.IsNaN(page.Height) || math.IsInf(page.Width, 0) || math.IsInf(page.Height, 0) {
			return fail(400, "证书页面尺寸无效或过大")
		}
		png, err := readCertificateOutput(dir, page.PreviewPath, "png")
		if err != nil {
			return err
		}
		asset := certificateAsset(u.ID, fmt.Sprintf("证书模板-第%d页", i+1), "png", png)
		assets, data = append(assets, asset), append(data, png)
		item.Pages = append(item.Pages, CertificatePage{Width: page.Width, Height: page.Height, PreviewFileID: asset.ID})
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	current, err := a.store.user(u.ID)
	if err != nil {
		return err
	}
	if !hasAnyPermission(current, "records") {
		return fail(403, "荣誉发放权限已变更，请刷新后重试")
	}
	if err := requireOwnedFiles(a.store, current, []string{in.SourceFileID}); err != nil {
		return err
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	paths := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, path := range paths {
				os.Remove(path)
			}
		}
	}()
	for i, asset := range assets {
		path, err := a.persistAsset(s, asset, data[i])
		if err != nil {
			return err
		}
		paths = append(paths, path)
	}
	if err = s.exec("INSERT INTO certificate_templates(id,created_by,payload) VALUES(?,?,?)", item.ID, item.CreatedBy, encoded(item)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	respond(w, 201, map[string]any{"item": item})
	return nil
}

func (a *App) updateCertificateTemplate(w http.ResponseWriter, r *http.Request, u User) error {
	item, err := a.certificateTemplate(u, r.PathValue("id"))
	if err != nil {
		return err
	}
	var in struct {
		Name   string             `json:"name"`
		Fields []CertificateField `json:"fields"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if !validCertificateName(in.Name) {
		return fail(400, "请填写有效的模板名称")
	}
	if err := validateCertificateFields(in.Fields, item.Pages); err != nil {
		return err
	}
	if in.Fields == nil {
		in.Fields = []CertificateField{}
	}
	item.Name, item.Fields, item.UpdatedAt = in.Name, in.Fields, now()
	if err := a.store.exec("UPDATE certificate_templates SET payload=? WHERE id=?", encoded(item), item.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": item})
	return nil
}

func certificateDirectionLabel(direction string) string {
	switch direction {
	case "电控":
		return "电控组"
	case "视觉算法":
		return "算法组"
	default:
		return direction + "组"
	}
}

func (a *App) certificateValues(u User, recipientID, direction, issuedAt string) (map[string]string, User, error) {
	var target User
	if direction != "" && !slices.Contains(technicalDirections, direction) || !validDate(issuedAt) {
		return nil, target, fail(400, "请选择有效组别和发放日期")
	}
	target, err := a.store.user(recipientID)
	if err != nil {
		return nil, target, err
	}
	allowed, err := a.recipientAllowed(u, target.ID, "records")
	if err != nil {
		return nil, target, err
	}
	if !allowed {
		return nil, target, fail(403, "只能向获授权组的成员发放荣誉")
	}
	directions, err := a.memberDirections(target.ID)
	if err != nil {
		return nil, target, err
	}
	if direction != "" {
		if !hasPermission(u, direction, "records") || !slices.Contains(directions, direction) {
			return nil, target, fail(403, "所选成员不在该组或没有该组的荣誉发放权限")
		}
		directions = []string{direction}
	}
	labels := []string{}
	for _, d := range technicalDirections {
		if slices.Contains(directions, d) && hasPermission(u, d, "records") {
			labels = append(labels, certificateDirectionLabel(d))
		}
	}
	profile, err := a.store.profile(target)
	if err != nil {
		return nil, target, err
	}
	stamp := time.Now().In(time.FixedZone("CST", 8*60*60))
	if issuedAt != "" {
		stamp, _ = time.Parse(time.RFC3339, issuedAt)
		stamp = stamp.In(time.FixedZone("CST", 8*60*60))
	}
	return map[string]string{"name": profile.Name, "direction": strings.Join(labels, " / "), "major": profile.Major, "className": profile.ClassName, "studentNumber": profile.StudentNumber, "date": stamp.Format("2006年01月02日")}, target, nil
}

func requireCertificateValues(fields []CertificateField, values map[string]string) error {
	for _, field := range fields {
		if field.Key != "text" && strings.TrimSpace(values[field.Key]) == "" {
			labels := map[string]string{"name": "姓名", "direction": "组别", "major": "专业", "className": "班级", "studentNumber": "学号", "date": "日期"}
			return fail(400, "收件人缺少"+labels[field.Key]+"，请先补全资料或移除该填写区域")
		}
	}
	return nil
}

func (a *App) renderCertificate(ctx context.Context, item CertificateTemplate, fields []CertificateField, values map[string]string, dir string, page int) (CertificateRenderResult, error) {
	var empty CertificateRenderResult
	f, source, err := a.openAsset(item.PDFFileID)
	if err != nil {
		return empty, err
	}
	path := f.Name()
	f.Close()
	return a.config.CertificateRenderer(ctx, CertificateRenderRequest{SourcePath: path, SourceName: source.Name, OutputDir: dir, Fields: fields, Values: values, PreviewPage: page})
}

func (a *App) previewCertificateTemplate(w http.ResponseWriter, r *http.Request, u User) error {
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	item, err := a.certificateTemplate(u, r.PathValue("id"))
	if err != nil {
		return err
	}
	var in struct {
		UserID    string              `json:"userId"`
		Direction string              `json:"direction"`
		IssuedAt  string              `json:"issuedAt"`
		Fields    *[]CertificateField `json:"fields"`
		Page      int                 `json:"page"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.Page < 0 || in.Page >= len(item.Pages) {
		return fail(400, "预览页码无效")
	}
	fields := item.Fields
	if in.Fields != nil {
		fields = *in.Fields
	}
	if err := validateCertificateFields(fields, item.Pages); err != nil {
		return err
	}
	values, _, err := a.certificateValues(u, in.UserID, in.Direction, in.IssuedAt)
	if err != nil {
		return err
	}
	if err := requireCertificateValues(fields, values); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "mr-certificate-preview-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	result, err := a.renderCertificate(r.Context(), item, fields, values, dir, in.Page)
	if err != nil {
		return err
	}
	png, err := readCertificateOutput(dir, result.PreviewPath, "png")
	if err != nil {
		return err
	}
	// Scope may have changed while the renderer ran. Recheck before returning
	// any personalized data, even though previews never persist a record.
	current, err := a.store.user(u.ID)
	if err != nil {
		return err
	}
	if _, err := a.certificateTemplate(current, item.ID); err != nil {
		return err
	}
	currentValues, _, err := a.certificateValues(current, in.UserID, in.Direction, in.IssuedAt)
	if err != nil {
		return err
	}
	if encoded(values) != encoded(currentValues) {
		return fail(409, "收件人资料已更新，请重新预览")
	}
	visibleValues := map[string]string{}
	for _, field := range fields {
		if field.Key != "text" {
			visibleValues[field.Key] = values[field.Key]
		}
	}
	respond(w, 200, map[string]any{"previewDataUrl": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), "values": visibleValues})
	return nil
}

func (a *App) existingCertificateBatch(u User, requestID, requestHash string) (certificateBatch, bool, error) {
	batch, err := readOne[certificateBatch](a.store, "SELECT payload FROM certificate_batches WHERE created_by=? AND request_id=?", u.ID, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return batch, false, nil
	}
	if err != nil {
		return batch, false, err
	}
	if batch.RequestHash != requestHash {
		return batch, false, fail(409, "该发放请求编号已用于其他内容，请重新发起发放")
	}
	for _, record := range batch.Items {
		allowed, err := a.recipientAllowed(u, record.UserID, "records")
		if err != nil {
			return batch, false, err
		}
		if !allowed {
			return batch, false, fail(403, "发放权限已变更，无法读取该批次")
		}
	}
	return batch, true, nil
}

func respondCertificateBatch(w http.ResponseWriter, batch certificateBatch, replayed bool) {
	status := 201
	if replayed {
		status = 200
	}
	respond(w, status, map[string]any{"batchId": batch.ID, "items": batch.Items, "replayed": replayed})
}

func (a *App) issueCertificateBatch(w http.ResponseWriter, r *http.Request, u User) error {
	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if !hasAnyPermission(u, "records") {
		return fail(403, "没有荣誉发放权限")
	}
	var in certificateBatchInput
	if err := decode(r, &in); err != nil {
		return err
	}
	in.Title, in.Description = strings.TrimSpace(in.Title), strings.TrimSpace(in.Description)
	if in.Title == "" || len(in.Title) > 300 || len(in.Description) > 10000 || !slices.Contains([]string{"certificate", "award", "notice"}, in.Kind) || !validDate(in.IssuedAt) {
		return fail(400, "请填写有效的证书标题、类型和发放日期")
	}
	if len(in.RequestID) < 16 || len(in.RequestID) > 100 || strings.Trim(in.RequestID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
		return fail(400, "缺少有效的发放请求编号")
	}
	if in.TemplateVersion == "" || !validDate(in.TemplateVersion) {
		return fail(400, "缺少证书模板版本，请重新预览后发放")
	}
	if len(in.UserIDs) < 1 || len(in.UserIDs) > 50 {
		return fail(400, "每批请选择 1–50 位收件人")
	}
	seen := map[string]bool{}
	for _, id := range in.UserIDs {
		if id == "" || seen[id] {
			return fail(400, "收件人不能为空或重复")
		}
		seen[id] = true
	}
	// Recipient selection order is not part of a batch's identity.
	slices.Sort(in.UserIDs)
	requestHash := tokenHash(encoded(in))
	if batch, found, err := a.existingCertificateBatch(u, in.RequestID, requestHash); err != nil {
		return err
	} else if found {
		respondCertificateBatch(w, batch, true)
		return nil
	}
	item, err := a.certificateTemplate(u, in.TemplateID)
	if err != nil {
		return err
	}
	if item.UpdatedAt != in.TemplateVersion {
		return fail(409, "证书模板已更新，请重新预览后发放")
	}
	if len(item.Fields) == 0 {
		return fail(400, "请先设置证书上的姓名等填写区域并保存模板")
	}
	if err := validateCertificateFields(item.Fields, item.Pages); err != nil {
		return err
	}
	if in.IssuedAt == "" {
		in.IssuedAt = now()
	}
	type renderedRecipient struct {
		user   User
		values map[string]string
		assets []FileAsset
		paths  []string
	}
	recipients := []renderedRecipient{}
	for _, id := range in.UserIDs {
		values, target, err := a.certificateValues(u, id, in.Direction, in.IssuedAt)
		if err != nil {
			return err
		}
		if err := requireCertificateValues(item.Fields, values); err != nil {
			return fail(400, target.Name+"："+err.Error())
		}
		recipients = append(recipients, renderedRecipient{user: target, values: values})
	}
	dir, err := os.MkdirTemp("", "mr-certificate-batch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for i := range recipients {
		outputDir := filepath.Join(dir, fmt.Sprint(i))
		if err := os.Mkdir(outputDir, 0700); err != nil {
			return err
		}
		result, err := a.renderCertificate(r.Context(), item, item.Fields, recipients[i].values, outputDir, 0)
		if err != nil {
			return err
		}
		pdf, err := readCertificateOutput(outputDir, result.PDFPath, "pdf")
		if err != nil {
			return err
		}
		png, err := readCertificateOutput(outputDir, result.PreviewPath, "png")
		if err != nil {
			return err
		}
		name := "证书-" + recipients[i].values["name"]
		if !validAssetName(name + ".pdf") {
			name = "个人证书"
		}
		recipients[i].assets = []FileAsset{certificateAsset(recipients[i].user.ID, name, "pdf", pdf), certificateAsset(recipients[i].user.ID, name+"-预览", "png", png)}
		recipients[i].paths = []string{result.PDFPath, result.PreviewPath}
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	u, err = a.store.user(u.ID)
	if err != nil {
		return err
	}
	if !hasAnyPermission(u, "records") {
		return fail(403, "荣誉发放权限已变更，请刷新后重试")
	}
	if batch, found, err := a.existingCertificateBatch(u, in.RequestID, requestHash); err != nil {
		return err
	} else if found {
		respondCertificateBatch(w, batch, true)
		return nil
	}
	currentTemplate, err := a.certificateTemplate(u, item.ID)
	if err != nil {
		return err
	}
	if currentTemplate.UpdatedAt != item.UpdatedAt {
		return fail(409, "证书模板已更新，请重新预览后发放")
	}
	for _, recipient := range recipients {
		values, _, err := a.certificateValues(u, recipient.user.ID, in.Direction, in.IssuedAt)
		if err != nil {
			return err
		}
		if encoded(values) != encoded(recipient.values) {
			return fail(409, "收件人资料已变更，请重新预览后发放")
		}
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	batch := certificateBatch{ID: randomID(), RequestHash: requestHash, Items: []Record{}}
	paths := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, path := range paths {
				os.Remove(path)
			}
		}
	}()
	for _, recipient := range recipients {
		record := Record{ID: randomID(), UserID: recipient.user.ID, UserName: recipient.user.Name, Title: in.Title, Kind: in.Kind, Description: in.Description, IssuedAt: in.IssuedAt, CreatedAt: now(), CreatedBy: u.ID, FileIDs: []string{}}
		for i, asset := range recipient.assets {
			format := "pdf"
			if i == 1 {
				format = "png"
			}
			data, err := readCertificateOutput(dir, recipient.paths[i], format)
			if err != nil {
				return err
			}
			path, err := a.persistAsset(s, asset, data)
			if err != nil {
				return err
			}
			paths = append(paths, path)
			record.FileIDs = append(record.FileIDs, asset.ID)
			if err := s.exec("INSERT INTO file_grants(file_id,user_id) VALUES(?,?) ON CONFLICT(file_id,user_id) DO NOTHING", asset.ID, recipient.user.ID); err != nil {
				return err
			}
		}
		if err := s.exec("INSERT INTO records(id,user_id,payload) VALUES(?,?,?)", record.ID, record.UserID, encoded(record)); err != nil {
			return err
		}
		batch.Items = append(batch.Items, record)
	}
	if err := s.exec("INSERT INTO certificate_batches(id,created_by,request_id,payload) VALUES(?,?,?,?)", batch.ID, u.ID, in.RequestID, encoded(batch)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	respondCertificateBatch(w, batch, false)
	return nil
}
