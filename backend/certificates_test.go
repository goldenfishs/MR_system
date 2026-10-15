package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeCertificateRenderer(ctx context.Context, in CertificateRenderRequest) (CertificateRenderResult, error) {
	if err := ctx.Err(); err != nil {
		return CertificateRenderResult{}, err
	}
	pdfPath, pngPath := filepath.Join(in.OutputDir, "result.pdf"), filepath.Join(in.OutputDir, "preview.png")
	png, _ := base64.StdEncoding.DecodeString(testPhoto().ContentBase64)
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7\n"+encoded(in.Values)), 0600); err != nil {
		return CertificateRenderResult{}, err
	}
	if err := os.WriteFile(pngPath, png, 0600); err != nil {
		return CertificateRenderResult{}, err
	}
	return CertificateRenderResult{PDFPath: pdfPath, PreviewPath: pngPath, Pages: []CertificateRenderedPage{{Width: 842, Height: 595, PreviewPath: pngPath}}}, nil
}

func certificateTemplateForTest(t *testing.T, c *testClient) CertificateTemplate {
	t.Helper()
	source := uploadForTest(t, c, "certificate.pdf", []byte("%PDF-1.7\nsource template"), 201)
	item := unmarshal[CertificateTemplate](t, c.request("POST", "/api/certificate-templates", map[string]string{"name": "入队证书", "sourceFileId": source.ID}, 201)["item"])
	if item.PDFFileID == source.ID || len(item.Pages) != 1 || item.Pages[0].PreviewFileID == "" {
		t.Fatal("template did not create normalized PDF and private page preview")
	}
	fields := []CertificateField{{ID: "name", Key: "name", Page: 0, X: .2, Y: .3, Width: .6, FontSize: 22, Align: "center", Color: "#123456"}, {ID: "direction", Key: "direction", Page: 0, X: .2, Y: .4, Width: .6, FontSize: 18, Align: "left", Color: "#000000"}}
	return unmarshal[CertificateTemplate](t, c.request("PATCH", "/api/certificate-templates/"+item.ID, map[string]any{"name": item.Name, "fields": fields}, 200)["item"])
}

func certificateMemberForTest(t *testing.T, a *App, c *testClient, name, email, direction string) User {
	t.Helper()
	u := unmarshal[User](t, c.request("POST", "/api/members", map[string]any{"name": name, "email": email, "password": "Certificate!2026", "role": "member", "memberType": "formal", "direction": direction}, 201)["item"])
	p := Profile{UserID: u.ID, Name: u.Name, Major: "机械设计制造及其自动化", ClassName: "机器人 1 班", StudentNumber: "20260001"}
	if err := a.store.saveProfile(p); err != nil {
		t.Fatal(err)
	}
	return u
}

func countCertificateRows(t *testing.T, a *App, table string) int {
	t.Helper()
	var n int
	if err := a.store.row("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCertificateBatchPersonalizationPrivacyAndIdempotency(t *testing.T) {
	a, admin := setup(t)
	a.config.CertificateRenderer = fakeCertificateRenderer
	admin.login("admin")
	one := certificateMemberForTest(t, a, admin, "机械同学甲", "one@certificate.test", "机械")
	two := certificateMemberForTest(t, a, admin, "机械同学乙", "two@certificate.test", "机械")
	item := certificateTemplateForTest(t, admin)
	before := countCertificateRows(t, a, "files")
	preview := admin.request("POST", "/api/certificate-templates/"+item.ID+"/preview", map[string]any{"userId": one.ID, "direction": "机械", "issuedAt": "2026-10-15T00:00:00+08:00"}, 200)
	values := unmarshal[map[string]string](t, preview["values"])
	if values["name"] != one.Name || values["direction"] != "机械组" || !strings.HasPrefix(unmarshal[string](t, preview["previewDataUrl"]), "data:image/png;base64,") {
		t.Fatalf("preview did not substitute member data: %v", values)
	}
	if countCertificateRows(t, a, "files") != before || countCertificateRows(t, a, "records") != 0 {
		t.Fatal("preview unexpectedly issued or persisted a certificate")
	}
	in := certificateBatchInput{TemplateID: item.ID, TemplateVersion: item.UpdatedAt, UserIDs: []string{one.ID, two.ID}, Title: "新生考核通过", Kind: "certificate", Direction: "机械", IssuedAt: "2026-10-15T00:00:00+08:00", RequestID: "certificate-batch-0001"}
	out := admin.request("POST", "/api/certificate-batches", in, 201)
	records := unmarshal[[]Record](t, out["items"])
	if len(records) != 2 || countCertificateRows(t, a, "records") != 2 || countCertificateRows(t, a, "files") != before+4 {
		t.Fatal("batch failed to issue separate PDF and PNG files to each recipient")
	}
	byUser := map[string]Record{}
	for _, record := range records {
		byUser[record.UserID] = record
		if len(record.FileIDs) != 2 {
			t.Fatal("certificate missing PDF/PNG pair")
		}
		pdf := downloadForTest(t, admin, record.FileIDs[0], "download", 200)
		if !strings.Contains(pdf.Body.String(), record.UserName) || !strings.Contains(pdf.Body.String(), "机械组") {
			t.Fatal("generated file was not personalized")
		}
		if png := downloadForTest(t, admin, record.FileIDs[1], "preview", 200); !strings.HasPrefix(png.Header().Get("Content-Type"), "image/png") {
			t.Fatal("generated thumbnail cannot be previewed inline")
		}
	}
	// A retry with the same people in a different order returns the original
	// records; changing the payload under a reused request ID is rejected.
	in.UserIDs = []string{two.ID, one.ID}
	replay := admin.request("POST", "/api/certificate-batches", in, 200)
	if !unmarshal[bool](t, replay["replayed"]) || string(replay["batchId"]) != string(out["batchId"]) || countCertificateRows(t, a, "records") != 2 {
		t.Fatal("batch retry created duplicate personal honors")
	}
	in.Title = "different"
	admin.request("POST", "/api/certificate-batches", in, 409)
	student := &testClient{t: t, handler: admin.handler}
	student.request("POST", "/api/auth/login", map[string]string{"email": one.Email, "password": "Certificate!2026"}, 200)
	mine := unmarshal[[]Record](t, student.request("GET", "/api/records", nil, 200)["items"])
	if len(mine) != 1 || mine[0].UserID != one.ID {
		t.Fatal("student record list exposed other recipients")
	}
	for _, id := range byUser[one.ID].FileIDs {
		downloadForTest(t, student, id, "download", 200)
	}
	for _, id := range byUser[two.ID].FileIDs {
		downloadForTest(t, student, id, "download", 403)
	}
	for _, id := range []string{item.SourceFileID, item.PDFFileID, item.Pages[0].PreviewFileID} {
		downloadForTest(t, student, id, "download", 403)
	}
	student.request("GET", "/api/certificate-templates", nil, 403)
	student.request("POST", "/api/certificate-batches", in, 403)
}

func TestCertificateGroupScopeAndTemplateOwnership(t *testing.T) {
	a, admin := setup(t)
	a.config.CertificateRenderer = fakeCertificateRenderer
	admin.login("admin")
	mechanical := certificateMemberForTest(t, a, admin, "机械组队员", "mechanical@certificate.test", "机械")
	electrical := certificateMemberForTest(t, a, admin, "控制组队员", "electrical@certificate.test", "电控")
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"records"}}})
	private := certificateTemplateForTest(t, admin)
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	if items := unmarshal[[]CertificateTemplate](t, staff.request("GET", "/api/certificate-templates", nil, 200)["items"]); len(items) != 0 {
		t.Fatal("another admin's template exposed to group manager")
	}
	staff.request("PATCH", "/api/certificate-templates/"+private.ID, map[string]any{"name": "forged", "fields": private.Fields}, 403)
	staff.request("POST", "/api/certificate-templates", map[string]string{"name": "forged", "sourceFileId": private.SourceFileID}, 403)
	item := certificateTemplateForTest(t, staff)
	staff.request("POST", "/api/certificate-templates/"+item.ID+"/preview", map[string]any{"userId": mechanical.ID}, 403)
	in := certificateBatchInput{TemplateID: item.ID, TemplateVersion: item.UpdatedAt, UserIDs: []string{electrical.ID, mechanical.ID}, Title: "证书", Kind: "certificate", RequestID: "scope-certificate-batch-01"}
	staff.request("POST", "/api/certificate-batches", in, 403)
	if countCertificateRows(t, a, "records") != 0 {
		t.Fatal("unauthorized batch partially issued certificates")
	}
	in.UserIDs = []string{electrical.ID}
	in.Direction = "机械"
	staff.request("POST", "/api/certificate-batches", in, 403)
	in.Direction = "电控"
	issued := unmarshal[[]Record](t, staff.request("POST", "/api/certificate-batches", in, 201)["items"])
	if len(issued) != 1 || issued[0].UserID != electrical.ID {
		t.Fatal("group manager could not issue within own scope")
	}
	admin.request("PATCH", "/api/members/"+electrical.ID, map[string]string{"direction": "机械"}, 200)
	staff.request("POST", "/api/certificate-batches", in, 403)
	downloadForTest(t, staff, issued[0].FileIDs[0], "download", 403)
}

func TestCertificateFailureRollsBackWholeBatch(t *testing.T) {
	a, admin := setup(t)
	a.config.CertificateRenderer = fakeCertificateRenderer
	admin.login("admin")
	one := certificateMemberForTest(t, a, admin, "甲", "rollback-one@certificate.test", "机械")
	two := certificateMemberForTest(t, a, admin, "乙", "rollback-two@certificate.test", "机械")
	item := certificateTemplateForTest(t, admin)
	beforeFiles := countCertificateRows(t, a, "files")
	beforeDisk, _ := os.ReadDir(a.uploadRoot())
	in := certificateBatchInput{TemplateID: item.ID, TemplateVersion: item.UpdatedAt, UserIDs: []string{one.ID, two.ID}, Title: "证书", Kind: "certificate", RequestID: "rollback-certificate-batch"}
	count := 0
	a.config.CertificateRenderer = func(ctx context.Context, in CertificateRenderRequest) (CertificateRenderResult, error) {
		count++
		if count == 2 {
			return CertificateRenderResult{}, fail(422, "测试渲染失败")
		}
		return fakeCertificateRenderer(ctx, in)
	}
	admin.request("POST", "/api/certificate-batches", in, 422)
	if countCertificateRows(t, a, "records") != 0 || countCertificateRows(t, a, "files") != beforeFiles || countCertificateRows(t, a, "certificate_batches") != 0 {
		t.Fatal("failed renderer left a partial batch")
	}
	a.config.CertificateRenderer = fakeCertificateRenderer
	// A database failure after generated assets have reached disk must remove
	// those assets as well as roll back the database rows.
	if err := a.store.exec("CREATE TRIGGER fail_certificate_record BEFORE INSERT ON records BEGIN SELECT RAISE(ABORT, 'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	admin.request("POST", "/api/certificate-batches", in, 500)
	afterDisk, _ := os.ReadDir(a.uploadRoot())
	if countCertificateRows(t, a, "records") != 0 || countCertificateRows(t, a, "files") != beforeFiles || len(afterDisk) != len(beforeDisk) {
		t.Fatal("failed transaction left generated assets on disk or partial records")
	}
	a.store.exec("DROP TRIGGER fail_certificate_record")
	admin.request("POST", "/api/certificate-batches", in, 201)
}

func TestCertificateValidatesFieldsRecipientsAndConcurrentChanges(t *testing.T) {
	a, admin := setup(t)
	a.config.CertificateRenderer = fakeCertificateRenderer
	admin.login("admin")
	member := certificateMemberForTest(t, a, admin, "检查同学", "validation@certificate.test", "机械")
	item := certificateTemplateForTest(t, admin)
	for label, mutate := range map[string]func(*CertificateField){
		"outside-page":      func(f *CertificateField) { f.X = .8; f.Width = .6 },
		"invalid-page":      func(f *CertificateField) { f.Page = 1 },
		"invalid-key":       func(f *CertificateField) { f.Key = "password" },
		"invalid-color":     func(f *CertificateField) { f.Color = "red" },
		"invalid-font-size": func(f *CertificateField) { f.FontSize = 200 },
	} {
		t.Run(label, func(t *testing.T) {
			field := item.Fields[0]
			mutate(&field)
			admin.request("PATCH", "/api/certificate-templates/"+item.ID, map[string]any{"name": item.Name, "fields": []CertificateField{field}}, 400)
		})
	}
	in := certificateBatchInput{TemplateID: item.ID, TemplateVersion: item.UpdatedAt, UserIDs: []string{member.ID, member.ID}, Title: "证书", Kind: "award", RequestID: "validate-certificate-batch"}
	admin.request("POST", "/api/certificate-batches", in, 400)
	in.UserIDs = []string{member.ID}
	validVersion := in.TemplateVersion
	in.TemplateVersion = ""
	admin.request("POST", "/api/certificate-batches", in, 400)
	in.TemplateVersion = "2026-01-01T00:00:00Z"
	admin.request("POST", "/api/certificate-batches", in, 409)
	in.TemplateVersion = validVersion
	fields := append([]CertificateField{}, item.Fields...)
	fields[0].Key = "studentNumber"
	memberProfile, _ := a.store.profile(member)
	memberProfile.StudentNumber = ""
	a.store.saveProfile(memberProfile)
	admin.request("POST", "/api/certificate-templates/"+item.ID+"/preview", map[string]any{"userId": member.ID, "fields": fields}, 400)
	a.config.CertificateRenderer = func(ctx context.Context, in CertificateRenderRequest) (CertificateRenderResult, error) {
		// Simulate editing the template during a slow render; saving an outdated
		// batch must fail without creating even one recipient record.
		changed := item
		changed.UpdatedAt = "changed-during-render"
		if err := a.store.exec("UPDATE certificate_templates SET payload=? WHERE id=?", encoded(changed), changed.ID); err != nil {
			return CertificateRenderResult{}, err
		}
		return fakeCertificateRenderer(ctx, in)
	}
	admin.request("POST", "/api/certificate-batches", in, 409)
	if countCertificateRows(t, a, "records") != 0 {
		t.Fatal("stale template issued a record")
	}
}

func TestCertificateNormalizationRejectsInvalidRendererAndUnsupportedSource(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	source := uploadForTest(t, admin, "source.pdf", []byte("%PDF-1.7\ntemplate"), 201)
	text := uploadForTest(t, admin, "source.txt", []byte("not a template"), 201)
	admin.request("POST", "/api/certificate-templates", map[string]string{"name": "wrong", "sourceFileId": text.ID}, 400)
	before := countCertificateRows(t, a, "files")
	a.config.CertificateRenderer = func(ctx context.Context, in CertificateRenderRequest) (CertificateRenderResult, error) {
		result, err := fakeCertificateRenderer(ctx, in)
		if err != nil {
			return result, err
		}
		for len(result.Pages) < 6 {
			result.Pages = append(result.Pages, result.Pages[0])
		}
		return result, nil
	}
	admin.request("POST", "/api/certificate-templates", map[string]string{"name": "too many pages", "sourceFileId": source.ID}, 400)
	if countCertificateRows(t, a, "files") != before {
		t.Fatal("invalid template was partially saved")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.pdf")
	os.WriteFile(outside, []byte("%PDF-1.7\noutside"), 0600)
	if _, err := readCertificateOutput(dir, outside, "pdf"); err == nil {
		t.Fatal("renderer escaped its temporary directory")
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.pdf")); err != nil {
		t.Fatal(err)
	}
	if _, err := readCertificateOutput(dir, filepath.Join(dir, "linked.pdf"), "pdf"); err == nil {
		t.Fatal("renderer output symlink was accepted")
	}
	valid := filepath.Join(dir, "valid.pdf")
	os.WriteFile(valid, []byte("%PDF-1.7\nvalid"), 0600)
	alias := filepath.Join(t.TempDir(), "directory-alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readCertificateOutput(alias, resolved, "pdf"); err != nil {
		t.Fatalf("valid output with canonical directory alias rejected: %v", err)
	}
	// Ensure all API field errors stay structured instead of returning renderer
	// internals to the browser.
	err = validateCertificateFields([]CertificateField{{ID: "x", Key: "text", Page: 0}}, []CertificatePage{{Width: 842, Height: 595}})
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.status != 400 {
		t.Fatal(fmt.Sprintf("expected structured validation error, got %v", err))
	}
	if _, err := json.Marshal(CertificateRenderRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestCertificateConcurrentRetryAndPermissionRevocation(t *testing.T) {
	a, admin := setup(t)
	a.config.CertificateRenderer = fakeCertificateRenderer
	admin.login("admin")
	member := certificateMemberForTest(t, a, admin, "并发测试", "concurrent@certificate.test", "电控")
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"records"}}})
	staff := &testClient{t: t, handler: admin.handler}
	staff.login("mentor")
	item := certificateTemplateForTest(t, staff)
	in := certificateBatchInput{TemplateID: item.ID, TemplateVersion: item.UpdatedAt, UserIDs: []string{member.ID}, Title: "并发证书", Kind: "certificate", RequestID: "concurrent-certificate-batch"}
	started, release := make(chan struct{}, 2), make(chan struct{})
	a.config.CertificateRenderer = func(ctx context.Context, in CertificateRenderRequest) (CertificateRenderResult, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return CertificateRenderResult{}, ctx.Err()
		}
		return fakeCertificateRenderer(ctx, in)
	}
	first := asyncRequest(admin.handler, "POST", "/api/certificate-batches", in, staff.cookie)
	second := asyncRequest(admin.handler, "POST", "/api/certificate-batches", in, staff.cookie)
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("certificate render unexpectedly serialized under the global write lock")
		}
	}
	// A normal unrelated write remains responsive while certificate renderers
	// are running; neither renderer can publish duplicate records on completion.
	admin.request("PATCH", "/api/settings", map[string]string{"announcement": "rendering in background"}, 200)
	close(release)
	r1, r2 := <-first, <-second
	if r1.Code+r2.Code != 401 || (r1.Code != 200 && r1.Code != 201) || (r2.Code != 200 && r2.Code != 201) {
		t.Fatalf("concurrent retries got %d/%d: %s / %s", r1.Code, r2.Code, r1.Body.String(), r2.Body.String())
	}
	if countCertificateRows(t, a, "records") != 1 || countCertificateRows(t, a, "certificate_batches") != 1 {
		t.Fatal("concurrent retries created duplicate recipient records")
	}

	// Revoke a staff member's permissions while a second batch is rendering.
	// This must neither block revocation nor permit an outdated grant to commit.
	started, release = make(chan struct{}, 1), make(chan struct{})
	in.RequestID = "permission-revocation-batch"
	pending := asyncRequest(admin.handler, "POST", "/api/certificate-batches", in, staff.cookie)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("certificate renderer did not start")
	}
	grantForTest(t, admin, "demo-mentor", []GroupGrant{{"电控", []string{"knowledge"}}})
	close(release)
	awaitStatus(t, pending, 403)
	if countCertificateRows(t, a, "records") != 1 || countCertificateRows(t, a, "certificate_batches") != 1 {
		t.Fatal("revoked issuer published an in-flight certificate batch")
	}
}
