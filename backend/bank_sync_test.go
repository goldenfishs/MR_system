package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bankSyncFixture(t *testing.T, a *App) bankSyncState {
	t.Helper()
	banks, err := loadQuestionBanks()
	if err != nil {
		t.Fatal(err)
	}
	records := []bankSyncRecord{}
	for i, q := range banks[0].Questions {
		if i == 3 {
			break
		}
		task := q.task(User{Role: "admin"})
		task.ID = ""
		task.CreatedBy = ""
		task.CreatedAt = ""
		if err = a.validateTaskContent(&task, User{Role: "admin"}); err != nil {
			t.Fatal(err)
		}
		records = append(records, bankSyncRecord{q.ID, task, []string{"cpp"}})
	}
	state, _ := loadBankSyncState(a.store)
	state.Settings.Catalogs = []string{"cpp"}
	state.Snapshot = &bankSyncSnapshot{strings.Repeat("a", 40), state.Settings, records}
	if err = saveBankSyncState(a.store, state); err != nil {
		t.Fatal(err)
	}
	return state
}
func TestBankSyncAuthorizationAndVisibilityRollback(t *testing.T) {
	a, c := setup(t)
	c.request("GET", "/api/bank-sync", nil, 401)
	c.login("student")
	c.request("GET", "/api/bank-sync", nil, 403)
	c.request("POST", "/api/bank-sync/preview", map[string]any{}, 403)
	c.request("PUT", "/api/bank-sync/settings", map[string]any{}, 403)
	c.request("POST", "/api/bank-sync/apply", map[string]any{}, 403)
	c.request("POST", "/api/bank-sync/visibility", map[string]any{}, 403)
	c.login("mentor")
	c.request("GET", "/api/bank-sync", nil, 403)
	c.login("admin")
	state := bankSyncFixture(t, a)
	c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": state.Snapshot.Commit}, 200)
	stored, _ := loadBankSyncState(a.store)
	id := stored.Mappings[state.Settings.Repository+"/"+state.Snapshot.Records[0].Key].TaskID
	before, _ := a.store.task(id)
	c.request("POST", "/api/bank-sync/visibility", map[string]any{"ids": []string{id, "missing"}, "visibility": "public"}, 404)
	after, _ := a.store.task(id)
	if encoded(before) != encoded(after) {
		t.Fatal("partial visibility change")
	}
	c.request("POST", "/api/bank-sync/visibility", map[string]any{"ids": []string{id}, "visibility": "public"}, 200)
	after, _ = a.store.task(id)
	before.BankVisibility = "public"
	if encoded(before) != encoded(after) {
		t.Fatal("visibility altered content")
	}
	view := c.request("GET", "/api/bank-sync", nil, 200)
	if strings.Contains(string(view["questions"]), "answers") || strings.Contains(string(view["questions"]), "testCases") {
		t.Fatal("summary leaked answers")
	}
}
func TestBankSyncStableMergePreservesLocalEditsAndResults(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	state := bankSyncFixture(t, a)
	commit := state.Snapshot.Commit
	res := c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": commit}, 200)
	if unmarshal[int](t, res["imported"]) != 3 {
		t.Fatal("not imported")
	}
	c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": commit}, 409)
	stored, _ := loadBankSyncState(a.store)
	stored.Snapshot = state.Snapshot
	saveBankSyncState(a.store, stored)
	res = c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": commit}, 200)
	if unmarshal[int](t, res["unchanged"]) != 3 {
		t.Fatal("not idempotent")
	}
	stored, _ = loadBankSyncState(a.store)
	stored.Snapshot = state.Snapshot
	first := stored.Mappings[state.Settings.Repository+"/"+state.Snapshot.Records[0].Key].TaskID
	local, _ := a.store.task(first)
	local.Title = "本地修改"
	a.store.exec("UPDATE tasks SET payload=? WHERE id=?", encoded(local), first)
	second := stored.Mappings[state.Settings.Repository+"/"+state.Snapshot.Records[1].Key].TaskID
	a.store.exec("INSERT INTO judge_runs(id,task_id,user_id,status,payload) VALUES('run',?,'student','accepted','{}')", second)
	for i := range stored.Snapshot.Records {
		stored.Snapshot.Records[i].Task.Description += "\n上游更新"
	}
	saveBankSyncState(a.store, stored)
	res = c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": commit}, 200)
	if unmarshal[int](t, res["conflicts"]) != 2 || unmarshal[int](t, res["updated"]) != 1 {
		t.Fatal("protected merge failed", res)
	}
	actual, _ := a.store.task(first)
	if actual.Title != "本地修改" {
		t.Fatal("overwrote local content")
	}
}
func TestBankSyncAdoptsExistingAndAtomicImport(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	state := bankSyncFixture(t, a)
	legacy := state.Snapshot.Records[0].Task
	legacy.ID = "previous-random-import"
	legacy.CreatedAt = now()
	legacy.BankVisibility = "public"
	a.store.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", legacy.ID, encoded(legacy))
	badID := tokenHash("mr-bank-sync/" + state.Settings.Repository + "/" + state.Snapshot.Records[2].Key)
	a.store.exec("CREATE TRIGGER fail_sync BEFORE INSERT ON tasks WHEN NEW.id='" + badID + "' BEGIN SELECT RAISE(ABORT,'forced'); END")
	c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": state.Snapshot.Commit}, 500)
	stored, _ := loadBankSyncState(a.store)
	if len(stored.Mappings) != 0 || stored.LastSync != nil {
		t.Fatal("metadata partially persisted")
	}
	var count int
	a.store.row("SELECT COUNT(*) FROM tasks WHERE id=?", tokenHash("mr-bank-sync/"+state.Settings.Repository+"/"+state.Snapshot.Records[1].Key)).Scan(&count)
	if count != 0 {
		t.Fatal("partial import")
	}
	a.store.exec("DROP TRIGGER fail_sync")
	res := c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": state.Snapshot.Commit}, 200)
	if unmarshal[int](t, res["imported"]) != 2 || unmarshal[int](t, res["unchanged"]) != 1 {
		t.Fatal("duplicated prior import")
	}
	stored, _ = loadBankSyncState(a.store)
	if stored.Mappings[state.Settings.Repository+"/"+state.Snapshot.Records[0].Key].TaskID != legacy.ID {
		t.Fatal("lost existing identity")
	}
}
func TestBankSyncSettingsInvalidatesSnapshot(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	state := bankSyncFixture(t, a)
	settings := state.Settings
	settings.Repository = "http://127.0.0.1/secret"
	c.request("PUT", "/api/bank-sync/settings", settings, 400)
	settings = state.Settings
	settings.DefaultVisibility = "public"
	c.request("PUT", "/api/bank-sync/settings", settings, 200)
	c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": state.Snapshot.Commit}, 409)
}
func TestBankSyncArchiveRejectsTraversalAndLinks(t *testing.T) {
	for _, h := range []tar.Header{{Name: "root/../escape.json", Mode: 0600, Size: 2}, {Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}} {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&h)
		if h.Size > 0 {
			tw.Write([]byte("{}"))
		}
		tw.Close()
		gz.Close()
		if _, err := readBankSyncTar(b.Bytes()); err == nil {
			t.Fatal("accepted unsafe archive")
		}
	}
}
func TestBankSyncManifestValidation(t *testing.T) {
	a, _ := setup(t)
	state := bankSyncFixture(t, a)
	q := state.Snapshot.Records[0]
	q.Task.BankVisibility = "public"
	q.Task.ID = ""
	q.Task.CreatedAt = ""
	q.Task.CreatedBy = ""
	manifest := bankSyncManifest{SchemaVersion: 1, ID: "test", Entrypoints: []string{"cpp"}, Catalogs: map[string]string{"cpp": "catalogs/cpp.json"}, Questions: map[string]string{q.Key: "questions/q.json"}}
	files := map[string][]byte{"bank.json": []byte(encoded(manifest)), "catalogs/cpp.json": []byte(`{"schemaVersion":1,"id":"cpp","name":"C++","chapters":[{"questions":["` + q.Key + `"]}]}`), "questions/q.json": []byte(encoded(bankSyncRemoteQuestion{1, q.Key, 1, "public", q.Task}))}
	cats, records, err := a.parseBankSyncArchive(files, User{Role: "admin"})
	if err != nil || len(cats) != 1 || len(records) != 1 {
		t.Fatal(err)
	}
	files["catalogs/cpp.json"] = []byte(`{"schemaVersion":1,"id":"cpp","name":"C++","includes":["cpp"]}`)
	if _, _, err = a.parseBankSyncArchive(files, User{Role: "admin"}); err == nil {
		t.Fatal("accepted cycle")
	}
}

// Optional integration check for the sibling exported bank; CI always runs the self-contained fixtures above.
func TestBankSyncExportedRepository(t *testing.T) {
	root := "../../MR_question_bank"
	if _, err := os.Stat(root + "/bank.json"); err != nil {
		t.Skip("sibling export absent")
	}
	a, _ := setup(t)
	files := map[string][]byte{}
	filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && strings.HasSuffix(p, ".json") {
			rel, _ := filepath.Rel(root, p)
			files[filepath.ToSlash(rel)], _ = os.ReadFile(p)
		}
		return nil
	})
	cats, records, err := a.parseBankSyncArchive(files, User{Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) < 4 || len(records) < 392 {
		t.Fatal("incomplete export")
	}
	data, _ := json.Marshal(records)
	if bytes.Contains(data, []byte("referenceSolution")) {
		t.Fatal("maintenance solutions leaked into task")
	}
}

func TestBankSyncGitArchivePAX(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "commit"}}); err != nil {
		t.Fatal(err)
	}
	tw.WriteHeader(&tar.Header{Name: "bank-main/bank.json", Mode: 0600, Size: 2})
	tw.Write([]byte("{}"))
	tw.Close()
	gz.Close()
	files, err := readBankSyncTar(b.Bytes())
	if err != nil || string(files["bank.json"]) != "{}" {
		t.Fatal("git PAX metadata rejected", err)
	}
}

func TestBankSyncCatalogExpansionBounded(t *testing.T) {
	a, _ := setup(t)
	state := bankSyncFixture(t, a)
	q := state.Snapshot.Records[0]
	q.Task.BankVisibility = "public"
	q.Task.ID = ""
	q.Task.CreatedAt = ""
	q.Task.CreatedBy = ""
	manifest := bankSyncManifest{SchemaVersion: 1, ID: "test", Entrypoints: []string{"cat0"}, Catalogs: map[string]string{}, Questions: map[string]string{q.Key: "questions/q.json"}}
	files := map[string][]byte{"questions/q.json": []byte(encoded(bankSyncRemoteQuestion{1, q.Key, 1, "public", q.Task}))}
	// An acyclic duplicated chain previously performed 2^40 recursive expansions.
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("cat%d", i)
		filename := "catalogs/" + id + ".json"
		manifest.Catalogs[id] = filename
		c := map[string]any{"schemaVersion": 1, "id": id, "name": id}
		if i < 39 {
			next := fmt.Sprintf("cat%d", i+1)
			c["includes"] = []string{next, next}
		} else {
			c["chapters"] = []any{map[string]any{"questions": []string{q.Key}}}
		}
		files[filename] = []byte(encoded(c))
	}
	files["bank.json"] = []byte(encoded(manifest))
	cats, records, err := a.parseBankSyncArchive(files, User{Role: "admin"})
	if err != nil || len(cats) != 40 || len(records) != 1 {
		t.Fatal("catalog expansion failed", err)
	}
}

func TestBankSyncDoesNotAliasSourcesAndProtectsAssignments(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	state := bankSyncFixture(t, a)
	rec := state.Snapshot.Records[0]
	legacy := rec.Task
	legacy.ID = "legacy-one"
	a.store.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", legacy.ID, encoded(legacy))
	copyRec := rec
	copyRec.Key = "other-distinct-source"
	state.Snapshot.Records = []bankSyncRecord{rec, copyRec}
	saveBankSyncState(a.store, state)
	result := c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": state.Snapshot.Commit}, 200)
	if unmarshal[int](t, result["conflicts"]) != 1 || unmarshal[int](t, result["unchanged"]) != 1 {
		t.Fatal("aliased source identities", result)
	}
	stored, _ := loadBankSyncState(a.store)
	state.Snapshot.Records = state.Snapshot.Records[:1]
	state.Snapshot.Records[0].Task.Description += "上游改变"
	stored.Snapshot = state.Snapshot
	saveBankSyncState(a.store, stored)
	assignment := Task{ID: "assignment-ref", Category: "assignment", Questions: []TaskQuestion{{ID: "q", SourceProblemID: legacy.ID}}}
	a.store.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", assignment.ID, encoded(assignment))
	result = c.request("POST", "/api/bank-sync/apply", map[string]string{"commit": state.Snapshot.Commit}, 200)
	if unmarshal[int](t, result["conflicts"]) != 1 {
		t.Fatal("overwrote assigned question")
	}
}
func TestBankSyncLiveGitHub(t *testing.T) {
	if os.Getenv("MR_TEST_BANK_GITHUB") != "1" {
		t.Skip("live GitHub check opt-in")
	}
	a, _ := setup(t)
	state, _ := loadBankSyncState(a.store)
	commit, files, err := fetchBankSyncArchive(context.Background(), state.Settings)
	if err != nil {
		t.Fatal(err)
	}
	cats, records, err := a.parseBankSyncArchive(files, User{Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 392 || len(cats) < 4 {
		t.Fatal("incomplete live repository")
	}
	t.Logf("validated commit %s: %d questions", commit, len(records))
}
