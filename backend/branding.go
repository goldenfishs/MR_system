package main

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func canonicalCompetition(s string) string {
	if s == "ROBOCON" {
		return "RoboCon"
	}
	return s
}
func sameCompetition(a, b string) bool { return canonicalCompetition(a) == canonicalCompetition(b) }
func normalizeCompetitions(items []string) []string {
	result := []string{}
	for _, s := range items {
		s = canonicalCompetition(strings.TrimSpace(s))
		if !includesID(result, s) {
			result = append(result, s)
		}
	}
	return result
}
func validCompetitions(items []string) bool {
	if len(items) == 0 || len(items) > 2 {
		return false
	}
	for _, item := range items {
		if !validTeam(item) {
			return false
		}
	}
	return len(normalizeCompetitions(items)) == len(items)
}
func validLogoURL(s string) bool {
	if validURL(s) && s != "" {
		return true
	}
	parsed, err := url.Parse(s)
	return err == nil && len(s) <= 2000 && strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") && (!strings.HasPrefix(s, "/api/") || strings.HasPrefix(s, "/api/public/logo/")) && !strings.ContainsAny(s, "\\\r\n") && parsed.Scheme == "" && parsed.Host == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
func validateBrand(s Settings) error {
	if s.BrandName == "" || len(s.BrandName) > 150 || s.TeamName == "" || len(s.TeamName) > 100 || len(s.SecondaryTeamName) > 100 || !validLogoURL(s.LogoURL) || !validCompetitions(s.Competitions) {
		return fail(400, "请填写有效的品牌名称、队名、标志地址和赛事")
	}
	return nil
}
func defaultBrand(s *Settings) {
	if s.BrandName == "" {
		s.BrandName = "QUT机器人战队"
	}
	if s.LogoURL == "" {
		s.LogoURL = "/mark.svg"
	}
	if s.TeamName == "" {
		s.TeamName = "沧溟"
	}
	if s.SecondaryTeamName == "" {
		s.SecondaryTeamName = "MOVE"
	}
	if len(s.Competitions) == 0 {
		s.Competitions = []string{"RoboCon", "RoboMaster"}
	}
	switch s.Organization {
	case "", "机器人战队协作平台", "竞跃", "竞跃机器人实验室", "竞跃 · 战队协作系统", "竞跃 MR System", "竞跃 · 机器人战队协作平台":
		s.Organization = "青岛理工大学机器人战队"
	}
}
func migrateBranding(s *Store) error {
	settings, err := s.settings()
	if err != nil {
		return err
	}
	// Run once. Later deliberate blank optional values and custom brands are retained.
	var count int
	if err = s.row("SELECT COUNT(*) FROM metadata WHERE key='qut_branding_v4'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	defaultBrand(&settings)
	if err = s.saveSettings(settings); err != nil {
		return err
	}
	teams, err := readAll[Team](s, "SELECT payload FROM teams")
	if err != nil {
		return err
	}
	for _, team := range teams {
		normalizeTeam(&team)
		switch team.Name {
		case "竞跃 RoboMaster 战队 · 2027", "竞跃机器人战队", "竞跃战队":
			team.Name = "沧溟（MOVE）机器人战队"
			team.Competitions = []string{"RoboCon", "RoboMaster"}
			team.Competition = "RoboCon"
		}
		if err = s.exec("UPDATE teams SET payload=? WHERE id=?", encoded(team), team.ID); err != nil {
			return err
		}
	}
	return s.exec("INSERT INTO metadata(key,value) VALUES('qut_branding_v4','true') ON CONFLICT(key) DO NOTHING")
}
func normalizeTeam(t *Team) {
	if len(t.Competitions) == 0 && t.Competition != "" {
		t.Competitions = []string{t.Competition}
	}
	t.Competitions = normalizeCompetitions(t.Competitions)
	if len(t.Competitions) > 0 {
		t.Competition = t.Competitions[0]
	}
}

// Uploaded brand marks use a dedicated public endpoint. Ordinary profile,
// certificate and task images never gain public access through this route.
func (a *App) uploadBrandLogo(w http.ResponseWriter, r *http.Request, u User) error {
	if u.Role != "admin" {
		return fail(403, "只有管理员可替换品牌标志")
	}
	var in PhotoInput
	if err := decode(r, &in); err != nil {
		return err
	}
	asset, data, err := preparePhoto(in, u.ID)
	if err != nil {
		return err
	}
	if asset.MIME == "image/png" {
		asset.Name = "战队标志.png"
	} else {
		asset.Name = "战队标志.jpg"
	}
	settings, err := a.store.settings()
	if err != nil {
		return err
	}
	tx, err := a.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	transactional := &Store{db: a.store.db, postgres: a.store.postgres, tx: tx}
	path, err := a.persistAsset(transactional, asset, data)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.Remove(path)
		}
	}()
	settings.LogoURL = "/api/public/logo/" + asset.ID
	if err = transactional.saveSettings(settings); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	respond(w, 200, map[string]any{"settings": settings})
	return nil
}
func (a *App) publicBrandLogo(w http.ResponseWriter, r *http.Request, _ User) error {
	settings, err := a.store.settings()
	if err != nil {
		return err
	}
	if settings.LogoURL != "/api/public/logo/"+r.PathValue("id") {
		return fail(404, "标志不存在")
	}
	file, asset, err := a.openAsset(r.PathValue("id"))
	if err != nil {
		return err
	}
	defer file.Close()
	if !asset.CleanImage || (asset.MIME != "image/png" && asset.MIME != "image/jpeg") {
		return fail(404, "标志不存在")
	}
	w.Header().Set("Content-Type", asset.MIME)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, asset.Name, time.Time{}, file)
	return nil
}
