package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func uploadForTest(t *testing.T, c *testClient, name string, data []byte, status int) FileAsset {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	r := httptest.NewRequest("POST", "/api/files", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("upload %s: got %d expected %d: %s", name, w.Code, status, w.Body.String())
	}
	if status != 201 {
		return FileAsset{}
	}
	var result struct {
		Item FileAsset `json:"item"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Item
}
func downloadForTest(t *testing.T, c *testClient, id, operation string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/files/"+id+"/"+operation, nil)
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("file %s: got %d expected %d: %s", operation, w.Code, status, w.Body.String())
	}
	return w
}
func completeProfile(t *testing.T, c *testClient) Profile {
	t.Helper()
	p := testProfileFields()
	return unmarshal[Profile](t, c.request("PATCH", "/api/profile", map[string]any{"name": "完整资料学员", "gender": p.Gender, "university": p.University, "major": p.Major, "className": p.ClassName, "studentNumber": p.StudentNumber, "phone": p.Phone, "hobbies": "机器人", "photo": testPhoto()}, 200)["profile"])
}
func TestRegistrationRequiresProfileAndPhotoAtomically(t *testing.T) {
	a, c := emailSetup(t)
	email := "profile@example.com"
	code := c.verification(email)
	body := registrationBody(email, code)
	delete(body, "profile")
	c.request("POST", "/api/auth/register", body, 400)
	body["profile"] = testProfileFields()
	delete(body, "photo")
	c.request("POST", "/api/auth/register", body, 400)
	body["photo"] = PhotoInput{Name: "fake.png", ContentBase64: base64.StdEncoding.EncodeToString([]byte("not an image"))}
	c.request("POST", "/api/auth/register", body, 400)
	body["photo"] = testPhoto()
	if err := a.store.exec(`CREATE TRIGGER fail_profile BEFORE INSERT ON profiles BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/auth/register", body, 500)
	for _, table := range []string{"users", "files", "profiles"} {
		var count int
		if err := a.store.row("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed registration leaked %s", table)
		}
	}
	files, err := os.ReadDir(a.uploadRoot())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("failed registration leaked photo bytes")
	}
	if err = a.store.exec("DROP TRIGGER fail_profile"); err != nil {
		t.Fatal(err)
	}
	user := unmarshal[User](t, c.request("POST", "/api/auth/register", body, 201)["user"])
	p := unmarshal[Profile](t, c.request("GET", "/api/profile", nil, 200)["profile"])
	if !p.Complete || p.UserID != user.ID || p.StudentNumber == "" || p.PhotoFileID == "" {
		t.Fatalf("registration did not persist complete profile: %+v", p)
	}
	response := downloadForTest(t, c, p.PhotoFileID, "preview", 200)
	if response.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "inline") {
		t.Fatal("sanitized avatar preview missing")
	}
}
func TestFilesValidateSizeTypeAndOwnership(t *testing.T) {
	a, c := setup(t)
	uploadForTest(t, c, "unauth.pdf", []byte("%PDF-1.7\nexample"), 401)
	c.login("student")
	uploadForTest(t, c, "fake.pdf", []byte("not a PDF"), 400)
	uploadForTest(t, c, "payload.exe", []byte("executable"), 400)
	uploadForTest(t, c, "empty.zip", nil, 400)
	uploadForTest(t, c, "large.zip", bytes.Repeat([]byte{0}, maxAssetSize+1), 413)
	asset := uploadForTest(t, c, "my-assembly.zip", []byte("opaque archive contents"), 201)
	got := downloadForTest(t, c, asset.ID, "download", 200)
	if got.Body.String() != "opaque archive contents" || got.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(got.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("file download lost bytes or safety headers")
	}
	svg := uploadForTest(t, c, "drawing.svg", []byte(`<svg><script>alert(1)</script></svg>`), 201)
	downloadForTest(t, c, svg.ID, "preview", 400)
	c.login("mentor")
	downloadForTest(t, c, asset.ID, "download", 403)
	c.request("GET", "/api/files/"+asset.ID, nil, 403)
	c.login("admin")
	downloadForTest(t, c, asset.ID, "download", 200)
	c.login("student")
	if err := a.store.exec("UPDATE files SET size=? WHERE id=?", maxUserStorage, asset.ID); err != nil {
		t.Fatal(err)
	}
	uploadForTest(t, c, "over-quota.txt", []byte("x"), 413)
}
func TestTeamAdmissionAndRecordPrivacy(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	team := unmarshal[Team](t, c.request("POST", "/api/teams", map[string]any{"name": "2026 校赛战队", "description": "面向所有学生招新", "competition": "RoboMaster", "directions": []string{"机械", "电控"}, "status": "open"}, 201)["item"])
	certificate := uploadForTest(t, c, "certificate.pdf", []byte("%PDF-1.7\ncertificate"), 201)
	c.login("student")
	c.request("POST", "/api/teams", map[string]any{}, 403)
	resume := uploadForTest(t, c, "resume.pdf", []byte("%PDF-1.7\nresume"), 201)
	payload := map[string]any{"direction": "电控", "resumeFileId": resume.ID, "motivation": "参加研发"}
	c.request("POST", "/api/teams/"+team.ID+"/applications", payload, 400)
	profile := completeProfile(t, c)
	if !profile.Complete {
		t.Fatal("profile still incomplete")
	}
	payload["resumeFileId"] = certificate.ID
	c.request("POST", "/api/teams/"+team.ID+"/applications", payload, 403)
	payload["resumeFileId"] = resume.ID
	application := unmarshal[TeamApplication](t, c.request("POST", "/api/teams/"+team.ID+"/applications", payload, 201)["item"])
	c.request("PATCH", "/api/team-applications/"+application.ID, map[string]string{"status": "accepted"}, 403)
	// A distinct student cannot read another student's application or files.
	other := unmarshal[User](t, c.register("另一学员", "other-team@example.com", "StrongPassword!2026")["user"])
	visible := unmarshal[[]TeamApplication](t, c.request("GET", "/api/teams/"+team.ID+"/applications", nil, 200)["items"])
	if len(visible) != 0 {
		t.Fatal("team applications leaked")
	}
	downloadForTest(t, c, resume.ID, "download", 403)
	c.login("admin")
	apps := unmarshal[[]TeamApplication](t, c.request("GET", "/api/teams/"+team.ID+"/applications", nil, 200)["items"])
	if len(apps) != 1 || apps[0].Profile == nil || apps[0].Profile.StudentNumber == "" {
		t.Fatal("admin lacks review profile")
	}
	c.request("PATCH", "/api/team-applications/"+application.ID, map[string]string{"status": "accepted", "feedback": "欢迎加入"}, 200)
	c.request("PATCH", "/api/team-applications/"+application.ID, map[string]string{"status": "accepted"}, 200)
	var members int
	if err := a.store.row("SELECT COUNT(*) FROM team_memberships WHERE team_id=?", team.ID).Scan(&members); err != nil || members != 1 {
		t.Fatal("admission not idempotent")
	}
	record := unmarshal[Record](t, c.request("POST", "/api/records", map[string]any{"userId": "demo-student", "title": "优秀队员奖状", "kind": "certificate", "fileIds": []string{certificate.ID}}, 201)["item"])
	c.login("student")
	c.request("POST", "/api/teams/"+team.ID+"/applications", payload, 409)
	mine := unmarshal[[]Membership](t, c.request("GET", "/api/memberships", nil, 200)["items"])
	if len(mine) != 1 || mine[0].TeamID != team.ID {
		t.Fatal("admission failed to grant membership")
	}
	downloadForTest(t, c, certificate.ID, "download", 200)
	records := unmarshal[[]Record](t, c.request("GET", "/api/records", nil, 200)["items"])
	if len(records) != 1 || records[0].ID != record.ID {
		t.Fatal("personal record missing")
	}
	c.request("GET", "/api/users", nil, 403)
	c.request("POST", "/api/auth/login", map[string]string{"email": other.Email, "password": "StrongPassword!2026"}, 200)
	records = unmarshal[[]Record](t, c.request("GET", "/api/records", nil, 200)["items"])
	if len(records) != 0 {
		t.Fatal("other user's personal records leaked")
	}
	downloadForTest(t, c, certificate.ID, "download", 403)
	c.login("admin")
	c.request("PATCH", "/api/team-applications/"+application.ID, map[string]string{"status": "rejected", "feedback": "撤销录取"}, 200)
	if err := a.store.row("SELECT COUNT(*) FROM team_memberships WHERE team_id=?", team.ID).Scan(&members); err != nil || members != 0 {
		t.Fatal("revoked admission retained membership")
	}
	c.request("PATCH", "/api/teams/"+team.ID, map[string]string{"status": "closed"}, 200)
	c.login("student")
	c.request("POST", "/api/teams/"+team.ID+"/applications", payload, 409)
}
func TestPlatformMigrationPreservesLegacyAccounts(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := createUser(s, "历史成员", "legacy-platform@example.com", "LegacyPassword!2026", "student", "RoboMaster", "机械")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"file_grants", "records", "team_memberships", "team_applications", "teams", "profiles", "files", "mail_campaigns", "judge_runs"} {
		if err = s.exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.exec("DELETE FROM schema_migrations WHERE version=3"); err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	s, err = openStore("", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	actual, err := s.user(legacy.ID)
	if err != nil || actual != legacy {
		t.Fatal("platform migration changed legacy identity")
	}
	p, err := s.profile(actual)
	if err != nil || p.Complete || p.Name != legacy.Name {
		t.Fatal("legacy profile should require explicit completion")
	}
	var version int
	if err = s.row("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 3 {
		t.Fatal("platform migration missing")
	}
}

func TestPhotoMetadataAndExecutablePayloadAreRemoved(t *testing.T) {
	a, c := setup(t)
	c.login("student")
	photo := testPhoto()
	raw, err := base64.StdEncoding.DecodeString(photo.ContentBase64)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("PRIVATE_EXIF_OR_SCRIPT_PAYLOAD")...)
	image := uploadForTest(t, c, "metadata.png", raw, 201)
	response := downloadForTest(t, c, image.ID, "preview", 200)
	if bytes.Contains(response.Body.Bytes(), []byte("PRIVATE_EXIF_OR_SCRIPT_PAYLOAD")) {
		t.Fatal("image re-encoding did not strip foreign payload")
	}
	if image.Size != int64(response.Body.Len()) {
		t.Fatal("asset size not updated to sanitized bytes")
	}
	profile := completeProfile(t, c)
	before, err := a.store.profile(User{ID: profile.UserID, Name: profile.Name})
	if err != nil {
		t.Fatal(err)
	}
	p := testProfileFields()
	p.Major = "机械工程"
	c.request("PATCH", "/api/profile", map[string]any{"name": "更新姓名", "gender": p.Gender, "university": p.University, "major": p.Major, "className": p.ClassName, "studentNumber": p.StudentNumber, "phone": p.Phone, "hobbies": "模型"}, 200)
	after := unmarshal[Profile](t, c.request("GET", "/api/profile", nil, 200)["profile"])
	if after.PhotoFileID != before.PhotoFileID || after.Major != p.Major || after.Name != "更新姓名" {
		t.Fatal("updating existing profile lost photo or failed to update identity")
	}
}
