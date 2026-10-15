package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuestionBankCatalogImportAndPreserveEdits(t *testing.T) {
	a, admin := setup(t)
	admin.login("admin")
	catalog := admin.request("GET", "/api/question-banks", nil, 200)
	for _, secret := range []string{"answers", "explanation", "testCases", "questions"} {
		if strings.Contains(string(catalog["items"]), `"`+secret+`"`) {
			t.Fatalf("catalog leaks %s", secret)
		}
	}
	banks, err := loadQuestionBanks()
	if err != nil {
		t.Fatal(err)
	}
	if len(banks) != 3 {
		t.Fatal("missing banks")
	}
	for _, bank := range banks {
		if bank.Count < 20 {
			t.Fatalf("bank %s missing content", bank.ID)
		}
		path := "/api/question-banks/" + bank.ID + "/import"
		result := admin.request("POST", path, map[string]any{}, 200)
		if unmarshal[int](t, result["imported"]) != bank.Count {
			t.Fatal("incomplete import")
		}
		first := bank.Questions[0].task(User{})
		admin.request("PATCH", "/api/tasks/"+first.ID, map[string]any{"title": "管理员修改后保留的题目"}, 200)
		result = admin.request("POST", path, map[string]any{}, 200)
		if unmarshal[int](t, result["imported"]) != 0 || unmarshal[int](t, result["skipped"]) != bank.Count {
			t.Fatal("import not idempotent")
		}
		actual, e := a.store.task(first.ID)
		if e != nil || actual.Title != "管理员修改后保留的题目" {
			t.Fatal("import overwrote edits")
		}
		for _, q := range bank.Questions {
			item, e := a.store.task(q.task(User{}).ID)
			if e != nil {
				t.Fatal(e)
			}
			if item.Required || item.Category != "problem" || item.Direction != bank.Direction {
				t.Fatal("practice imported as compulsory assignment")
			}
			public := publicTask(item, User{Role: "freshman"})
			if public.MechanicalProblem != nil && (len(public.MechanicalProblem.Answers) > 0 || public.MechanicalProblem.Explanation != "") {
				t.Fatal("student answer leakage")
			}
			if public.Judge != nil {
				for _, c := range public.Judge.TestCases {
					if c.Hidden {
						t.Fatal("student hidden-case leakage")
					}
				}
			}
		}
	}
}

func TestQuestionBankScopeAndInvalidBank(t *testing.T) {
	_, c := setup(t)
	c.request("GET", "/api/question-banks", nil, 401)
	c.login("student")
	c.request("GET", "/api/question-banks", nil, 403)
	c.request("POST", "/api/question-banks/algorithm-basics-v1/import", nil, 403)
	c.login("mentor") // migrated electric-control administrator
	items := unmarshal[[]questionBank](t, c.request("GET", "/api/question-banks", nil, 200)["items"])
	if len(items) != 1 || items[0].Direction != "电控" {
		t.Fatal("catalog crossed scope")
	}
	c.request("POST", "/api/question-banks/algorithm-basics-v1/import", nil, 403)
	c.request("POST", "/api/question-banks/hardware-basics-v1/import", nil, 403)
	c.request("POST", "/api/question-banks/unknown/import", nil, 404)
	c.request("POST", "/api/question-banks/mcu-basics-v1/import", nil, 200)
}

func TestQuestionBankImportRollsBackOnFailure(t *testing.T) {
	a, c := setup(t)
	c.login("admin")
	// Force a real mid-batch storage failure, not a validation error.
	banks, err := loadQuestionBanks()
	if err != nil {
		t.Fatal(err)
	}
	last := banks[0].Questions[2].task(User{}).ID
	if err = a.store.exec("CREATE TRIGGER fail_bank_insert BEFORE INSERT ON tasks WHEN NEW.id='" + last + "' BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/question-banks/algorithm-basics-v1/import", nil, 500)
	var count int
	if err = a.store.row("SELECT COUNT(*) FROM tasks WHERE id=?", banks[0].Questions[0].task(User{}).ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial import persisted")
	}
	if err = a.store.exec("DROP TRIGGER fail_bank_insert"); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/question-banks/algorithm-basics-v1/import", json.RawMessage(`{}`), 200)
}
