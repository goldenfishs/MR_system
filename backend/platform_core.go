package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
)

const maxAssetSize = 25 << 20
const maxUserStorage = 250 << 20
const maxPhotoSize = 2 << 20

type Profile struct {
	UserID        string `json:"userId"`
	Name          string `json:"name"`
	Gender        string `json:"gender"`
	University    string `json:"university"`
	Major         string `json:"major"`
	ClassName     string `json:"className"`
	StudentNumber string `json:"studentNumber"`
	Phone         string `json:"phone"`
	Hobbies       string `json:"hobbies"`
	AvatarFileID  string `json:"avatarFileId"`
	PhotoFileID   string `json:"photoFileId"`
	UpdatedAt     string `json:"updatedAt"`
	Complete      bool   `json:"complete"`
}
type ProfileFields struct {
	Gender        string `json:"gender"`
	University    string `json:"university"`
	Major         string `json:"major"`
	ClassName     string `json:"className"`
	StudentNumber string `json:"studentNumber"`
	Phone         string `json:"phone"`
	Hobbies       string `json:"hobbies"`
}
type PhotoInput struct {
	Name          string `json:"name"`
	ContentBase64 string `json:"contentBase64"`
}
type FileAsset struct {
	ID         string `json:"id"`
	OwnerID    string `json:"ownerId"`
	Name       string `json:"name"`
	MIME       string `json:"mime"`
	Size       int64  `json:"size"`
	CreatedAt  string `json:"createdAt"`
	CleanImage bool   `json:"-"`
}

// Internal metadata is separate from the API shape so filesystem or processing
// details never become part of the public file contract.
type storedFile struct {
	FileAsset
	CleanImage bool `json:"cleanImage"`
}
type Team struct {
	InterviewExemptDirections []string         `json:"interviewExemptDirections,omitempty"`
	ID                        string           `json:"id"`
	Name                      string           `json:"name"`
	Description               string           `json:"description"`
	Competition               string           `json:"competition"`
	Competitions              []string         `json:"competitions"`
	Directions                []string         `json:"directions"`
	Status                    string           `json:"status"`
	Deadline                  string           `json:"deadline"`
	CreatedAt                 string           `json:"createdAt"`
	CreatedBy                 string           `json:"createdBy"`
	MemberCount               int              `json:"memberCount"`
	MyApplication             *TeamApplication `json:"myApplication,omitempty"`
}
type TeamApplication struct {
	Interview          *TeamInterview `json:"interview,omitempty"`
	TrainingStatus     string         `json:"trainingStatus,omitempty"`
	TrainingStageTitle string         `json:"trainingStageTitle,omitempty"`
	TrainingCycle      int            `json:"trainingCycle,omitempty"`
	ID                 string         `json:"id"`
	TeamID             string         `json:"teamId"`
	UserID             string         `json:"userId"`
	UserName           string         `json:"userName"`
	Direction          string         `json:"direction"`
	ResumeFileID       string         `json:"resumeFileId"`
	Motivation         string         `json:"motivation"`
	Status             string         `json:"status"`
	Feedback           string         `json:"feedback"`
	CreatedAt          string         `json:"createdAt"`
	UpdatedAt          string         `json:"updatedAt"`
	Profile            *Profile       `json:"profile,omitempty"`
}
type Membership struct {
	TeamID    string `json:"teamId"`
	UserID    string `json:"userId"`
	Direction string `json:"direction"`
	JoinedAt  string `json:"joinedAt"`
}
type Record struct {
	ID          string   `json:"id"`
	UserID      string   `json:"userId"`
	UserName    string   `json:"userName"`
	Title       string   `json:"title"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	FileIDs     []string `json:"fileIds"`
	IssuedAt    string   `json:"issuedAt"`
	CreatedAt   string   `json:"createdAt"`
	CreatedBy   string   `json:"createdBy"`
}

func (a *App) registerPlatformRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/profile", a.wrap(true, a.getProfile))
	m.HandleFunc("PATCH /api/profile", a.wrap(true, a.updateProfile))
	m.HandleFunc("PATCH /api/profile/avatar", a.wrap(true, a.updateProfileAvatar))
	m.HandleFunc("GET /api/files", a.wrap(true, a.listFiles))
	m.HandleFunc("GET /api/files/{id}", a.wrap(true, a.getFileAsset))
	m.HandleFunc("POST /api/files", a.wrap(true, a.uploadFile))
	m.HandleFunc("GET /api/files/{id}/download", a.wrap(true, a.downloadFile))
	m.HandleFunc("GET /api/files/{id}/preview", a.wrap(true, a.previewFile))
	m.HandleFunc("GET /api/teams", a.wrap(true, a.listTeams))
	m.HandleFunc("POST /api/teams", a.wrap(true, a.createTeam))
	m.HandleFunc("PATCH /api/teams/{id}", a.wrap(true, a.updateTeam))
	m.HandleFunc("GET /api/teams/{id}/applications", a.wrap(true, a.listTeamApplications))
	m.HandleFunc("POST /api/teams/{id}/applications", a.wrap(true, a.applyToTeam))
	m.HandleFunc("PATCH /api/team-applications/{id}", a.wrap(true, a.reviewTeamApplication))
	m.HandleFunc("GET /api/memberships", a.wrap(true, a.listMemberships))
	m.HandleFunc("GET /api/users", a.wrap(true, a.listUsers))
	m.HandleFunc("POST /api/users/{id}/deactivate", a.wrap(true, a.deactivateUser))
	m.HandleFunc("POST /api/users/{id}/reactivate", a.wrap(true, a.reactivateUser))
	m.HandleFunc("GET /api/records", a.wrap(true, a.listRecords))
	m.HandleFunc("POST /api/records", a.wrap(true, a.createRecord))
	a.registerTrainingRoutes(m)
	a.registerInterviewRoutes(m)
}
func trimProfile(p *Profile) {
	p.Name = strings.TrimSpace(p.Name)
	p.Gender = strings.TrimSpace(p.Gender)
	p.University = strings.TrimSpace(p.University)
	p.Major = strings.TrimSpace(p.Major)
	p.ClassName = strings.TrimSpace(p.ClassName)
	p.StudentNumber = strings.TrimSpace(p.StudentNumber)
	p.Phone = strings.TrimSpace(p.Phone)
	p.Hobbies = strings.TrimSpace(p.Hobbies)
}
func validateProfile(p Profile) error {
	if p.Gender != "男" && p.Gender != "女" && p.Gender != "其他" {
		return fail(400, "请选择性别")
	}
	for _, v := range []string{p.Name, p.University, p.Major, p.ClassName, p.StudentNumber, p.Phone} {
		if v == "" || len(v) > 200 || strings.ContainsAny(v, "\r\n\x00") {
			return fail(400, "请完整填写姓名、学校、专业、班级、学号与联系电话")
		}
	}
	if len(p.Name) > 100 || len(p.Phone) > 40 || len(p.Hobbies) > 2000 {
		return fail(400, "个人资料内容过长")
	}
	return nil
}
func profileFromFields(name string, in ProfileFields) Profile {
	p := Profile{Name: name, Gender: in.Gender, University: in.University, Major: in.Major, ClassName: in.ClassName, StudentNumber: in.StudentNumber, Phone: in.Phone, Hobbies: in.Hobbies}
	trimProfile(&p)
	return p
}
func (s *Store) profile(u User) (Profile, error) {
	name := u.Name
	if name == unfilledProfileName {
		name = ""
	}
	p, err := readOne[Profile](s, "SELECT payload FROM profiles WHERE user_id=?", u.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{UserID: u.ID, Name: name}, nil
	}
	if err != nil {
		return p, err
	}
	if p.Name == "" {
		p.Name = name
	} else {
		p.Name = u.Name
	}
	p.Complete = validateProfile(p) == nil && p.PhotoFileID != ""
	return p, nil
}
func (s *Store) saveProfile(p Profile) error {
	return s.exec("INSERT INTO profiles(user_id,payload) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET payload=excluded.payload", p.UserID, encoded(p))
}
func (a *App) getProfile(w http.ResponseWriter, r *http.Request, u User) error {
	p, err := a.store.profile(u)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"profile": p})
	return nil
}
func (a *App) updateProfile(w http.ResponseWriter, r *http.Request, u User) error {
	var in struct {
		Name string `json:"name"`
		ProfileFields
		Photo  *PhotoInput `json:"photo"`
		Avatar *PhotoInput `json:"avatar"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	// Avatars are optional and independent of the mandatory identity/profile
	// fields. This lets an account choose an avatar before completing signup
	// information while keeping the full update contract backward compatible.
	if in.Avatar != nil && in.Name == "" && in.ProfileFields == (ProfileFields{}) && in.Photo == nil {
		p, err := a.updateAvatar(r.Context(), u, *in.Avatar)
		if err != nil {
			return err
		}
		respond(w, 200, map[string]any{"profile": p})
		return nil
	}
	p := profileFromFields(in.Name, in.ProfileFields)
	if err := validateProfile(p); err != nil {
		return err
	}
	old, err := a.store.profile(u)
	if err != nil {
		return err
	}
	p.UserID = u.ID
	p.AvatarFileID = old.AvatarFileID
	p.PhotoFileID = old.PhotoFileID
	p.UpdatedAt = now()
	var photo FileAsset
	var photoData []byte
	if in.Photo != nil {
		photo, photoData, err = preparePhoto(*in.Photo, u.ID)
		if err != nil {
			return err
		}
		p.PhotoFileID = photo.ID
	}
	var avatar FileAsset
	var avatarData []byte
	if in.Avatar != nil {
		avatar, avatarData, err = prepareAvatar(*in.Avatar, u.ID)
		if err != nil {
			return err
		}
		p.AvatarFileID = avatar.ID
	}
	if p.PhotoFileID == "" {
		return fail(400, "请上传本人证件照（JPEG 或 PNG，最大 2 MiB）")
	}
	p.Complete = true
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	committed := false
	savedPaths := []string{}
	defer func() {
		if !committed {
			for _, path := range savedPaths {
				_ = os.Remove(path)
			}
		}
	}()
	if in.Photo != nil {
		savedPath, persistErr := a.persistAsset(s, photo, photoData)
		err = persistErr
		if err != nil {
			return err
		}
		savedPaths = append(savedPaths, savedPath)
	}
	if in.Avatar != nil {
		savedPath, persistErr := a.persistAsset(s, avatar, avatarData)
		err = persistErr
		if err != nil {
			return err
		}
		savedPaths = append(savedPaths, savedPath)
	}
	if err = s.saveProfile(p); err != nil {
		return err
	}
	u.Name = p.Name
	if err = s.exec("UPDATE users SET payload=? WHERE id=?", encoded(u), u.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	respond(w, 200, map[string]any{"profile": p, "user": u})
	return nil
}

// PATCH /api/profile/avatar accepts a PhotoInput directly and is useful for a
// profile form that edits the avatar separately from identity information.
func (a *App) updateProfileAvatar(w http.ResponseWriter, r *http.Request, u User) error {
	var in PhotoInput
	if err := decode(r, &in); err != nil {
		return err
	}
	p, err := a.updateAvatar(r.Context(), u, in)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"profile": p})
	return nil
}

func (a *App) updateAvatar(ctx context.Context, u User, in PhotoInput) (Profile, error) {
	avatar, data, err := prepareAvatar(in, u.ID)
	if err != nil {
		return Profile{}, err
	}
	tx, err := a.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	path, err := a.persistAsset(s, avatar, data)
	if err != nil {
		return Profile{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(path)
		}
	}()
	p, err := s.profile(u)
	if err != nil {
		return Profile{}, err
	}
	p.AvatarFileID = avatar.ID
	p.UpdatedAt = now()
	if err = s.saveProfile(p); err != nil {
		return Profile{}, err
	}
	if err = tx.Commit(); err != nil {
		return Profile{}, err
	}
	committed = true
	return p, nil
}

func prepareAvatar(in PhotoInput, owner string) (FileAsset, []byte, error) {
	asset, data, err := preparePhoto(in, owner)
	if err != nil {
		return FileAsset{}, nil, err
	}
	if asset.MIME == "image/png" {
		asset.Name = "头像.png"
	} else {
		asset.Name = "头像.jpg"
	}
	return asset, data, nil
}
func preparePhoto(in PhotoInput, owner string) (FileAsset, []byte, error) {
	if len(in.ContentBase64) > base64.StdEncoding.EncodedLen(maxPhotoSize) {
		return FileAsset{}, nil, fail(413, "照片不能超过 2 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(in.ContentBase64)
	if err != nil || len(data) == 0 {
		return FileAsset{}, nil, fail(400, "照片数据无效")
	}
	if len(data) > maxPhotoSize {
		return FileAsset{}, nil, fail(413, "照片不能超过 2 MiB")
	}
	data, mimeType, err := sanitizeImage(data)
	if err != nil {
		return FileAsset{}, nil, err
	}
	name := "个人照片.jpg"
	if mimeType == "image/png" {
		name = "个人照片.png"
	}
	return FileAsset{ID: randomID(), OwnerID: owner, Name: name, MIME: mimeType, Size: int64(len(data)), CreatedAt: now(), CleanImage: true}, data, nil
}
func sanitizeImage(data []byte) ([]byte, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 6000 || cfg.Height > 6000 || int64(cfg.Width)*int64(cfg.Height) > 20_000_000 {
		return nil, "", fail(400, "请上传有效的 JPEG 或 PNG 图片，尺寸不超过 6000 像素及 2000 万像素")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fail(400, "图片无法解码")
	}
	var out bytes.Buffer
	mimeType := "image/jpeg"
	if format == "png" {
		err = png.Encode(&out, img)
		mimeType = "image/png"
	} else {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		return nil, "", err
	}
	if out.Len() > maxAssetSize {
		return nil, "", fail(413, "图片解码后的文件过大")
	}
	return out.Bytes(), mimeType, nil
}
func (a *App) uploadRoot() string {
	if a.config.UploadDir != "" {
		return a.config.UploadDir
	}
	return a.store.fileDir
}
func (a *App) persistAsset(s *Store, f FileAsset, data []byte) (string, error) {
	var used int64
	if err := s.row("SELECT COALESCE(SUM(size),0) FROM files WHERE owner_id=?", f.OwnerID).Scan(&used); err != nil {
		return "", err
	}
	if used+f.Size > maxUserStorage {
		return "", fail(413, "个人文件空间已达到 250 MiB 上限")
	}
	root := a.uploadRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(root, f.ID)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = file.Write(data)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	if err = s.exec("INSERT INTO files(id,owner_id,size,payload) VALUES(?,?,?,?)", f.ID, f.OwnerID, f.Size, encoded(storedFile{FileAsset: f, CleanImage: f.CleanImage})); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
func (a *App) fileAsset(id string) (FileAsset, error) {
	f, err := readOne[storedFile](a.store, "SELECT payload FROM files WHERE id=?", id)
	f.FileAsset.CleanImage = f.CleanImage
	return f.FileAsset, err
}
func (a *App) openAsset(id string) (*os.File, FileAsset, error) {
	f, err := a.fileAsset(id)
	if err != nil {
		return nil, f, err
	}
	// IDs are generated by randomID. Validate even stored IDs before filesystem use.
	if len(f.ID) != 40 || strings.Trim(f.ID, "0123456789abcdef") != "" {
		return nil, f, fail(404, "文件不存在")
	}
	file, err := os.Open(filepath.Join(a.uploadRoot(), f.ID))
	return file, f, err
}
func requireOwnedFiles(s *Store, u User, ids []string) error {
	if len(ids) > 20 {
		return fail(400, "每次最多选择 20 个文件")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return fail(400, "文件不能重复")
		}
		seen[id] = true
		var financeRefs int
		if err := s.row("SELECT COUNT(*) FROM finance_files WHERE file_id=?", id).Scan(&financeRefs); err != nil {
			return err
		}
		if financeRefs > 0 {
			return fail(400, "财务凭证仅用于财务业务；其他用途请单独上传文件")
		}
		f, err := readOne[FileAsset](s, "SELECT payload FROM files WHERE id=?", id)
		if errors.Is(err, sql.ErrNoRows) {
			return fail(400, "所选文件不存在")
		}
		if err != nil {
			return err
		}
		if f.OwnerID != u.ID && u.Role != "admin" {
			return fail(403, "只能使用自己上传的文件")
		}
	}
	return nil
}
func (a *App) canDownloadFile(u User, id string) (bool, error) {
	f, err := a.fileAsset(id)
	if err != nil {
		return false, err
	}
	// Finance receipts remain private even if a generic resource later references them.
	var financeRefs, financeAccess int
	if err = a.store.row("SELECT COUNT(*) FROM finance_files WHERE file_id=?", id).Scan(&financeRefs); err != nil {
		return false, err
	}
	if financeRefs > 0 {
		if u.Role == "admin" || f.OwnerID == u.ID {
			return true, nil
		}
		err = a.store.row("SELECT COUNT(*) FROM finance_files WHERE file_id=? AND user_id=?", id, u.ID).Scan(&financeAccess)
		return financeAccess > 0, err
	}
	if f.OwnerID == u.ID || u.Role == "admin" {
		return true, nil
	}
	var grants int
	if err = a.store.row("SELECT COUNT(*) FROM file_grants WHERE file_id=? AND user_id=?", id, u.ID).Scan(&grants); err != nil {
		return false, err
	}
	if grants > 0 {
		return true, nil
	}
	allowed, err := a.taskFileAccess(u, id)
	if err != nil || allowed {
		return allowed, err
	}
	allowed, err = a.contextualFileAccess(u, id)
	if err != nil || allowed {
		return allowed, err
	}
	return a.knowledgeFileAccess(u, id)
}
func validAssetName(name string) bool {
	if len(name) == 0 || len(name) > 240 || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func allowedExtension(name string) bool {
	name = strings.ToLower(filepath.Base(name))
	if slices.Contains([]string{"makefile", "gnumakefile", "dockerfile"}, name) {
		return true
	}
	// Creo appends numeric revision suffixes, e.g. bracket.prt.12.
	ext := filepath.Ext(name)
	if len(ext) > 1 && strings.Trim(ext[1:], "0123456789") == "" {
		return slices.Contains([]string{".prt", ".asm", ".drw"}, filepath.Ext(strings.TrimSuffix(name, ext)))
	}
	return slices.Contains([]string{".pdf", ".ofd", ".mp4", ".webm", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".md", ".zip", ".7z", ".rar", ".c", ".cpp", ".cc", ".cxx", ".cs", ".rb", ".php", ".kt", ".swift", ".lua", ".pas", ".hs", ".scala", ".m", ".ml", ".pl", ".sh", ".h", ".hpp", ".py", ".js", ".ts", ".java", ".go", ".rs", ".json", ".yaml", ".yml", ".png", ".jpg", ".jpeg", ".webp", ".svg", ".pcb", ".kicad_pcb", ".sch", ".kicad_sch", ".gerber", ".gbr", ".dxf", ".step", ".stp", ".sldprt", ".sldasm", ".ino", ".hex", ".bin", ".stl", ".f3d", ".pro", ".ioc", ".ipynb", ".csv", ".toml", ".cmake", ".epro", ".eprj",
		".slddrw", ".dwg", ".dwt", ".dwf", ".dws", ".iges", ".igs", ".x_t", ".x_b", ".xmt_txt", ".xmt_bin", ".prt", ".asm", ".drw", ".ipt", ".iam", ".idw", ".ipn", ".sat", ".sab", ".obj", ".mtl", ".3mf", ".ply", ".stpz", ".f3z", ".fcstd", ".catpart", ".catproduct", ".catdrawing", ".3dxml", ".jt",
		".hh", ".hxx", ".ipp", ".tpp", ".inl", ".s", ".asm", ".cu", ".cuh", ".pyi", ".pyw", ".jsx", ".tsx", ".html", ".css", ".scss", ".vue", ".r", ".jl", ".f", ".f90", ".v", ".sv", ".vh", ".svh", ".vhd", ".vhdl", ".ino", ".pde", ".ld", ".map", ".elf", ".axf", ".uvproj", ".uvprojx", ".uvopt", ".uvoptx", ".ewp", ".eww", ".ewd", ".cproject", ".project", ".sln", ".vcxproj", ".vcxproj.filters", ".mk", ".bat", ".ps1", ".xml", ".ini", ".cfg", ".conf", ".log", ".dat", ".npz", ".npy", ".mat", ".tar", ".gz", ".bz2", ".xz", ".tgz", ".schdoc", ".pcbdoc", ".prjpcb", ".pcblib", ".schlib", ".brd", ".drl", ".lbr", ".kicad_pro", ".kicad_sym", ".kicad_mod",
	}, ext)
}
func (a *App) uploadFile(w http.ResponseWriter, r *http.Request, u User) error {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		return fail(400, "请通过文件表单上传文件")
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 || len(r.MultipartForm.Value) > 0 {
		return fail(400, "每次请上传一个 file 文件")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return fail(400, "请选择文件")
	}
	defer file.Close()
	name := strings.TrimSpace(header.Filename)
	if !validAssetName(name) || !allowedExtension(name) {
		return fail(400, "不支持该文件名称或类型，请将其他工程格式打包为 ZIP 后上传")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAssetSize+1))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fail(400, "不能上传空文件")
	}
	if len(data) > maxAssetSize {
		return fail(413, "单个文件不能超过 25 MiB")
	}
	mimeType := "application/octet-stream"
	clean := false
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".pdf" {
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return fail(400, "PDF 文件格式无效")
		}
		mimeType = "application/pdf"
	}
	if ext == ".mp4" || ext == ".webm" {
		detected := http.DetectContentType(data)
		if ext == ".mp4" && detected != "video/mp4" || ext == ".webm" && detected != "video/webm" {
			return fail(400, "视频文件格式无效，请使用 MP4 或 WebM")
		}
		mimeType = detected
	}
	if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
		data, mimeType, err = sanitizeImage(data)
		if err != nil {
			return err
		}
		clean = true
	}
	asset := FileAsset{ID: randomID(), OwnerID: u.ID, Name: name, MIME: mimeType, Size: int64(len(data)), CreatedAt: now(), CleanImage: clean}
	if _, err = a.persistAsset(a.store, asset, data); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"item": asset})
	return nil
}
func (a *App) listFiles(w http.ResponseWriter, r *http.Request, u User) error {
	q := "SELECT payload FROM files WHERE owner_id=?"
	args := []any{u.ID}
	if r.URL.Query().Get("all") == "true" && u.Role == "admin" {
		q = "SELECT payload FROM files"
		args = nil
	}
	files, err := readAll[FileAsset](a.store, q+" ORDER BY id DESC", args...)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"items": files})
	return nil
}
func (a *App) serveFile(w http.ResponseWriter, r *http.Request, u User, preview bool) error {
	id := r.PathValue("id")
	allowed, err := a.canDownloadFile(u, id)
	if err != nil {
		return err
	}
	if !allowed {
		return fail(403, "无权访问该文件")
	}
	file, f, err := a.openAsset(id)
	if errors.Is(err, os.ErrNotExist) {
		return fail(404, "文件内容不存在")
	}
	if err != nil {
		return err
	}
	defer file.Close()
	disposition := "attachment"
	if preview {
		if !(f.CleanImage && (f.MIME == "image/jpeg" || f.MIME == "image/png")) && f.MIME != "application/pdf" && f.MIME != "video/mp4" && f.MIME != "video/webm" {
			return fail(400, "该文件不支持在线预览")
		}
		disposition = "inline"
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	}
	w.Header().Set("Content-Type", f.MIME)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.Name}))
	if preview && f.MIME == "application/pdf" {
		// Chromium renders PDFs through its built-in viewer, which cannot run under CSP sandbox.
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	} else if preview {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'; sandbox")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	http.ServeContent(w, r, f.Name, time.Time{}, file)
	return nil
}
func (a *App) downloadFile(w http.ResponseWriter, r *http.Request, u User) error {
	return a.serveFile(w, r, u, false)
}
func (a *App) previewFile(w http.ResponseWriter, r *http.Request, u User) error {
	return a.serveFile(w, r, u, true)
}

func validateTeam(t Team) error {
	if strings.TrimSpace(t.Name) == "" || len(t.Name) > 200 || len(t.Description) > 10000 || !validCompetitions(t.Competitions) || (t.Status != "open" && t.Status != "closed") || !validDate(t.Deadline) || len(t.Directions) == 0 || len(t.Directions) > 5 {
		return fail(400, "请填写有效团队名称、赛事、开放状态、截止时间与技术方向")
	}
	seen := map[string]bool{}
	for _, d := range t.Directions {
		if !validDirection(d) || seen[d] {
			return fail(400, "技术方向无效或重复")
		}
		seen[d] = true
	}
	return nil
}
func (a *App) listTeams(w http.ResponseWriter, r *http.Request, u User) error {
	teams, err := readAll[Team](a.store, "SELECT payload FROM teams ORDER BY id DESC")
	if err != nil {
		return err
	}
	for i := range teams {
		normalizeTeam(&teams[i])
		if err = a.store.row("SELECT COUNT(*) FROM team_memberships WHERE team_id=?", teams[i].ID).Scan(&teams[i].MemberCount); err != nil {
			return err
		}
		ap, e := readOne[TeamApplication](a.store, "SELECT payload FROM team_applications WHERE team_id=? AND user_id=?", teams[i].ID, u.ID)
		if e == nil {
			if err = annotateTrainingApplication(a.store, &ap); err != nil {
				return err
			}
			teams[i].MyApplication = &ap
		} else if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
	}
	respond(w, 200, map[string]any{"items": teams})
	return nil
}

type teamInput struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Competition  string   `json:"competition"`
	Competitions []string `json:"competitions"`
	Directions   []string `json:"directions"`
	Status       string   `json:"status"`
	Deadline     string   `json:"deadline"`
}

func (a *App) createTeam(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可发布团队")
	}
	var in teamInput
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.Status == "" {
		in.Status = "open"
	}
	t := Team{ID: randomID(), Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description), Competition: in.Competition, Competitions: in.Competitions, Directions: in.Directions, Status: in.Status, Deadline: in.Deadline, CreatedAt: now(), CreatedBy: u.ID}
	normalizeTeam(&t)
	if err := validateTeam(t); err != nil {
		return err
	}
	if err := a.store.exec("INSERT INTO teams(id,payload) VALUES(?,?)", t.ID, encoded(t)); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"item": t})
	return nil
}
func (a *App) updateTeam(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可编辑团队")
	}
	t, err := readOne[Team](a.store, "SELECT payload FROM teams WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	var in struct {
		Name                      *string   `json:"name"`
		Description               *string   `json:"description"`
		Competition               *string   `json:"competition"`
		Competitions              *[]string `json:"competitions"`
		Directions                *[]string `json:"directions"`
		Status                    *string   `json:"status"`
		Deadline                  *string   `json:"deadline"`
		InterviewExemptDirections *[]string `json:"interviewExemptDirections"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if in.Name != nil {
		t.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		t.Description = strings.TrimSpace(*in.Description)
	}
	if in.Competition != nil {
		t.Competition = *in.Competition
		t.Competitions = []string{*in.Competition}
	}
	if in.Competitions != nil {
		t.Competitions = *in.Competitions
	}
	normalizeTeam(&t)
	if in.Directions != nil {
		t.Directions = *in.Directions
	}
	if in.Status != nil {
		t.Status = *in.Status
	}
	if in.Deadline != nil {
		t.Deadline = *in.Deadline
	}
	if in.InterviewExemptDirections != nil {
		for _, d := range *in.InterviewExemptDirections {
			if !slices.Contains(t.Directions, d) {
				return fail(400, "面试特例必须属于团队开放组别")
			}
		}
		t.InterviewExemptDirections = normalizedIDs(*in.InterviewExemptDirections)
	}
	if err = validateTeam(t); err != nil {
		return err
	}
	if err = a.store.exec("UPDATE teams SET payload=? WHERE id=?", encoded(t), t.ID); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": t})
	return nil
}
func (a *App) applyToTeam(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role == "admin" {
		return fail(403, "请以成员账号提交报名")
	}
	t, err := readOne[Team](a.store, "SELECT payload FROM teams WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if t.Status != "open" || expired(t.Deadline) {
		return fail(409, "该团队报名已关闭")
	}
	p, err := a.store.profile(u)
	if err != nil {
		return err
	}
	if !p.Complete {
		return fail(400, "请先补全个人资料和照片")
	}
	var in struct {
		Direction    string `json:"direction"`
		ResumeFileID string `json:"resumeFileId"`
		Motivation   string `json:"motivation"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if !slices.Contains(t.Directions, in.Direction) || len(in.Motivation) > 5000 {
		return fail(400, "请选择团队开放的技术方向，报名说明最多 5000 字节")
	}
	if err = requireOwnedFiles(a.store, u, []string{in.ResumeFileID}); err != nil {
		return err
	}
	file, err := a.fileAsset(in.ResumeFileID)
	if err != nil {
		return err
	}
	if file.MIME != "application/pdf" {
		return fail(400, "简历必须为 PDF 文件")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = lockInterviewSlots(s); err != nil {
		return err
	}
	// Re-read within the booking lock so an interview exemption configured
	// concurrently is reflected in this application, rather than a stale team.
	t, err = readOne[Team](s, "SELECT payload FROM teams WHERE id=?", t.ID)
	if err != nil {
		return err
	}
	if t.Status != "open" || expired(t.Deadline) || !slices.Contains(t.Directions, in.Direction) {
		return fail(409, "该团队或组别已停止报名，请刷新后重试")
	}
	ap, err := readOne[TeamApplication](s, "SELECT payload FROM team_applications WHERE team_id=? AND user_id=?", t.ID, u.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && ap.Status == "accepted" {
		return fail(409, "你已加入该团队，不能修改报名")
	}
	if ap.ID == "" {
		ap = TeamApplication{ID: randomID(), TeamID: t.ID, UserID: u.ID, CreatedAt: now()}
	}
	if ap.Interview != nil && ap.Interview.At != "" {
		return fail(409, "已安排面试，请联系组管理员修改报名")
	}
	ap.UserName = u.Name
	ap.Direction = in.Direction
	ap.ResumeFileID = in.ResumeFileID
	ap.Motivation = strings.TrimSpace(in.Motivation)
	ap.Interview = &TeamInterview{Status: "pending"}
	if slices.Contains(t.InterviewExemptDirections, ap.Direction) {
		ap.Interview = &TeamInterview{Status: "exempt", Note: "本轮已完成线下面试（历史招募特例）", DecidedBy: t.CreatedBy, DecidedAt: now()}
	}
	ap.Status = "pending"
	ap.Feedback = ""
	ap.UpdatedAt = now()
	if err = s.exec("INSERT INTO team_applications(id,team_id,user_id,payload) VALUES(?,?,?,?) ON CONFLICT(team_id,user_id) DO UPDATE SET payload=excluded.payload", ap.ID, t.ID, u.ID, encoded(ap)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"item": ap})
	return nil
}
func (a *App) listTeamApplications(w http.ResponseWriter, r *http.Request, u User) error {
	apps, err := a.scopedTeamApplications(u, r.PathValue("id"))
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"items": apps})
	return nil
}
func (a *App) reviewTeamApplication(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "recruitment") {
		return fail(403, "没有报名审核权限")
	}
	var in struct {
		Status         string `json:"status"`
		Feedback       string `json:"feedback"`
		ReopenTraining bool   `json:"reopenTraining"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if (in.Status != "accepted" && in.Status != "rejected") || len(in.Feedback) > 5000 {
		return fail(400, "审核状态或反馈无效")
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = lockInterviewSlots(s); err != nil {
		return err
	}
	ap, err := readOne[TeamApplication](s, "SELECT payload FROM team_applications WHERE id=?", r.PathValue("id"))
	if err != nil {
		return err
	}
	if ap.UserID == u.ID && u.Role != "admin" {
		return fail(403, "组管理员不能审核自己的报名")
	}
	if err = requirePermission(u, ap.Direction, "recruitment"); err != nil {
		return err
	}
	if in.Status == "accepted" && ap.Status != "accepted" && ap.Interview != nil && ap.Interview.Status != "passed" && ap.Interview.Status != "exempt" {
		return fail(409, "请先在面试管理中确认面试通过，再录取入队")
	}
	ap.Status = in.Status
	ap.Feedback = strings.TrimSpace(in.Feedback)
	ap.UpdatedAt = now()
	if in.Status == "accepted" {
		if err = admitTraining(s, u, ap.UserID, ap.TeamID, ap.Direction, in.ReopenTraining); err != nil {
			return err
		}
		member := Membership{TeamID: ap.TeamID, UserID: ap.UserID, Direction: ap.Direction, JoinedAt: now()}
		if err = s.exec("INSERT INTO team_memberships(team_id,user_id,direction,joined_at,payload) VALUES(?,?,?,?,?) ON CONFLICT(team_id,user_id) DO UPDATE SET direction=excluded.direction,payload=excluded.payload", member.TeamID, member.UserID, member.Direction, member.JoinedAt, encoded(member)); err != nil {
			return err
		}
	} else {
		if err = s.exec("DELETE FROM team_memberships WHERE team_id=? AND user_id=?", ap.TeamID, ap.UserID); err != nil {
			return err
		}
	}
	if err = s.exec("UPDATE team_applications SET payload=? WHERE id=?", encoded(ap), ap.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = annotateTrainingApplication(a.store, &ap); err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": ap})
	return nil
}
func (a *App) listMemberships(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := a.scopedMemberships(u, "members")
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"items": items})
	return nil
}
func (a *App) listUsers(w http.ResponseWriter, r *http.Request, u User) error {
	permission := r.URL.Query().Get("permission")
	if permission == "" {
		permission = "members"
	}
	if !slices.Contains([]string{"members", "recruitment", "records", "mail"}, permission) {
		return fail(400, "无效的用户列表用途")
	}
	if !hasAnyPermission(u, permission) {
		return fail(403, "没有该模块的人员查询权限")
	}
	users, err := readAll[User](a.store, "SELECT payload FROM users ORDER BY email")
	if err != nil {
		return err
	}
	type userProfile struct {
		User
		Profile            Profile  `json:"profile"`
		EligibleDirections []string `json:"eligibleDirections,omitempty"`
	}
	items := []userProfile{}
	for _, user := range users {
		allowed, e := a.recipientAllowed(u, user.ID, permission)
		if e != nil {
			return e
		}
		if !allowed {
			continue
		}
		p, e := a.store.profile(user)
		if e != nil {
			return e
		}
		// Recipient pickers only need contact and identity fields. Full personal
		// details require a separate members or recruitment grant for this user.
		if u.Role != "admin" && u.ID != user.ID && (permission == "mail" || permission == "records") {
			memberAccess, e := a.recipientAllowed(u, user.ID, "members")
			if e != nil {
				return e
			}
			recruitmentAccess, e := a.recipientAllowed(u, user.ID, "recruitment")
			if e != nil {
				return e
			}
			if !memberAccess && !recruitmentAccess {
				studentNumber := ""
				if permission == "records" {
					studentNumber = p.StudentNumber
				}
				p = Profile{UserID: user.ID, Name: user.Name, StudentNumber: studentNumber}
				user.AssessmentPassedAt, user.AssessmentConfirmedBy, user.AssessmentNote = "", "", ""
				user.PromotionOverrideAt, user.PromotionOverrideBy, user.PromotionOverrideNote = "", "", ""
			}
		}
		eligibleDirections := []string{}
		if permission == "records" {
			directions, e := a.memberDirections(user.ID)
			if e != nil {
				return e
			}
			for _, direction := range directions {
				if hasPermission(u, direction, "records") {
					eligibleDirections = append(eligibleDirections, direction)
				}
			}
		}
		items = append(items, userProfile{User: user, Profile: p, EligibleDirections: eligibleDirections})
	}
	respond(w, 200, map[string]any{"items": items})
	return nil
}
func (a *App) listRecords(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := a.scopedRecords(u)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"items": items})
	return nil
}
func (a *App) createRecord(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "records") {
		return fail(403, "没有荣誉发放权限")
	}
	var in struct {
		UserID      string   `json:"userId"`
		Title       string   `json:"title"`
		Kind        string   `json:"kind"`
		Description string   `json:"description"`
		FileIDs     []string `json:"fileIds"`
		IssuedAt    string   `json:"issuedAt"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Title) == "" || len(in.Title) > 300 || len(in.Description) > 10000 || !slices.Contains([]string{"certificate", "award", "notice"}, in.Kind) || !validDate(in.IssuedAt) {
		return fail(400, "请填写有效的档案标题、类型和发放时间")
	}
	recipient, err := a.store.user(in.UserID)
	if err != nil {
		return err
	}
	allowed, err := a.recipientAllowed(u, recipient.ID, "records")
	if err != nil {
		return err
	}
	if !allowed {
		return fail(403, "只能向获授权组的成员发放荣誉")
	}
	if err = requireOwnedFiles(a.store, u, in.FileIDs); err != nil {
		return err
	}
	if in.FileIDs == nil {
		in.FileIDs = []string{}
	}
	if in.IssuedAt == "" {
		in.IssuedAt = now()
	}
	record := Record{ID: randomID(), UserID: recipient.ID, UserName: recipient.Name, Title: strings.TrimSpace(in.Title), Kind: in.Kind, Description: strings.TrimSpace(in.Description), FileIDs: in.FileIDs, IssuedAt: in.IssuedAt, CreatedAt: now(), CreatedBy: u.ID}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	if err = s.exec("INSERT INTO records(id,user_id,payload) VALUES(?,?,?)", record.ID, record.UserID, encoded(record)); err != nil {
		return err
	}
	for _, id := range in.FileIDs {
		if err = s.exec("INSERT INTO file_grants(file_id,user_id) VALUES(?,?) ON CONFLICT(file_id,user_id) DO NOTHING", id, recipient.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	respond(w, 201, map[string]any{"item": record})
	return nil
}

func (a *App) getFileAsset(w http.ResponseWriter, r *http.Request, u User) error {
	id := r.PathValue("id")
	allowed, err := a.canDownloadFile(u, id)
	if err != nil {
		return err
	}
	if !allowed {
		return fail(403, "无权访问该文件")
	}
	asset, err := a.fileAsset(id)
	if err != nil {
		return err
	}
	respond(w, 200, map[string]any{"item": asset})
	return nil
}
