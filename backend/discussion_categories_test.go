package main

import (
	"strings"
	"testing"
)

func TestDiscussionCategoryLifecyclePermissionsAndVisibility(t *testing.T) {
	_, c := setup(t)
	c.request("GET", "/api/discussion-categories", nil, 401)
	c.login("student")
	categories := unmarshal[[]discussionCategoryView](t, c.request("GET", "/api/discussion-categories", nil, 200)["categories"])
	if len(categories) != 4 || categories[0].Name != "学习答疑" || categories[0].CanManage {
		t.Fatal("default categories missing or grants leaked")
	}
	c.request("POST", "/api/discussion-categories", map[string]any{"name": "test"}, 403)
	c.request("PATCH", "/api/discussion-categories/discussion-learning", map[string]any{"name": "test"}, 403)
	c.request("DELETE", "/api/discussion-categories/discussion-learning", nil, 403)
	c.login("mentor")
	c.request("POST", "/api/discussion-categories", map[string]any{"name": "全局越权"}, 403)
	c.request("POST", "/api/discussion-categories", map[string]any{"name": "跨组越权", "direction": "机械"}, 403)
	category := unmarshal[DiscussionCategory](t, c.request("POST", "/api/discussion-categories", map[string]any{"name": "  单片机调试  ", "direction": "电控", "sortOrder": 5}, 201)["category"])
	if category.Name != "单片机调试" {
		t.Fatal("name not trimmed")
	}
	c.request("POST", "/api/discussion-categories", map[string]any{"name": "单片机调试", "direction": "电控"}, 409)
	c.request("PATCH", "/api/discussion-categories/"+category.ID, map[string]any{"direction": "机械"}, 400)
	c.login("interviewer")
	c.request("PATCH", "/api/discussion-categories/"+category.ID, map[string]any{"name": "跨组编辑"}, 403)
	c.request("DELETE", "/api/discussion-categories/"+category.ID, nil, 403)
	c.login("student")
	question := map[string]any{"title": "串口错误", "content": "如何排查？", "direction": "电控", "categoryId": category.ID}
	d := unmarshal[Discussion](t, c.request("POST", "/api/discussions", question, 201)["discussion"])
	if d.CategoryID != category.ID {
		t.Fatal("category not persisted")
	}
	question["direction"] = "机械"
	c.request("POST", "/api/discussions", question, 400)
	question["direction"] = ""
	c.request("POST", "/api/discussions", question, 400)
	question["direction"] = "电控"
	question["categoryId"] = "missing"
	c.request("POST", "/api/discussions", question, 400)
	c.request("PATCH", "/api/discussions/"+d.ID, map[string]any{"categoryId": ""}, 403)
	c.login("interviewer")
	for _, cat := range unmarshal[[]discussionCategoryView](t, c.request("GET", "/api/discussion-categories", nil, 200)["categories"]) {
		if cat.QuestionCount != 0 {
			t.Fatal("private question count leaked")
		}
	}
	if len(unmarshal[[]Discussion](t, c.request("GET", "/api/discussions?categoryId="+category.ID, nil, 200)["discussions"])) != 0 {
		t.Fatal("category filter leaked private question")
	}
	c.login("mentor")
	for _, cat := range unmarshal[[]discussionCategoryView](t, c.request("GET", "/api/discussion-categories", nil, 200)["categories"]) {
		if cat.ID == category.ID && (cat.QuestionCount != 1 || !cat.CanManage) {
			t.Fatal("manager counts incorrect")
		}
	}
	c.request("DELETE", "/api/discussion-categories/"+category.ID, nil, 409)
	c.request("PATCH", "/api/discussion-categories/"+category.ID, map[string]any{"archived": true, "name": "串口排错"}, 200)
	c.login("student")
	question["categoryId"] = category.ID
	c.request("POST", "/api/discussions", question, 400)
	listed := unmarshal[[]Discussion](t, c.request("GET", "/api/discussions?categoryId="+category.ID, nil, 200)["discussions"])
	if len(listed) != 1 || listed[0].ID != d.ID {
		t.Fatal("archival removed historical question")
	}
	c.login("mentor")
	// Editing status must not invalidate an existing archived assignment.
	c.request("PATCH", "/api/discussions/"+d.ID, map[string]any{"categoryId": category.ID, "status": "open"}, 200)
	c.request("PATCH", "/api/discussion-categories/"+category.ID, map[string]any{"archived": false}, 200)
	c.request("PATCH", "/api/discussions/"+d.ID, map[string]any{"categoryId": "discussion-learning"}, 200)
	c.request("DELETE", "/api/discussion-categories/"+category.ID, nil, 200)
	c.request("PATCH", "/api/discussions/"+d.ID, map[string]any{"categoryId": category.ID}, 400)
	c.request("PATCH", "/api/discussions/"+d.ID, map[string]any{"categoryId": ""}, 200)
	uncategorized := unmarshal[[]Discussion](t, c.request("GET", "/api/discussions?categoryId=__uncategorized", nil, 200)["discussions"])
	if len(uncategorized) != 1 || uncategorized[0].Published || uncategorized[0].Status != "open" {
		t.Fatal("recategorization changed visibility or status")
	}
	c.login("admin")
	c.request("DELETE", "/api/discussion-categories/discussion-learning", nil, 200)
	cats := unmarshal[[]discussionCategoryView](t, c.request("GET", "/api/discussion-categories", nil, 200)["categories"])
	if len(cats) != 3 {
		t.Fatal("default category respawned after removal")
	}
}
func TestDiscussionCategoryValidationAndPublication(t *testing.T) {
	_, c := setup(t)
	c.login("admin")
	for _, body := range []map[string]any{
		{"name": ""}, {"name": strings.Repeat("中", 31)}, {"name": "a\nb"}, {"name": "未分类"}, {"name": "test", "description": strings.Repeat("中", 201)}, {"name": "test", "sortOrder": -1}, {"name": "test", "sortOrder": 10000}, {"name": "test", "direction": "无效"},
	} {
		c.request("POST", "/api/discussion-categories", body, 400)
	}
	c.request("POST", "/api/discussion-categories", map[string]any{"name": "学习答疑", "direction": "机械"}, 201)
	c.login("student")
	d := unmarshal[Discussion](t, c.request("POST", "/api/discussions", map[string]any{"title": "课程疑问", "content": "解释一下", "direction": "电控", "categoryId": "discussion-learning"}, 201)["discussion"])
	c.login("mentor")
	c.request("POST", "/api/discussions/"+d.ID+"/replies", map[string]any{"content": "解答内容"}, 201)
	c.request("PATCH", "/api/discussions/"+d.ID, map[string]any{"status": "resolved", "published": true}, 200)
	c.login("interviewer")
	for _, cat := range unmarshal[[]discussionCategoryView](t, c.request("GET", "/api/discussion-categories", nil, 200)["categories"]) {
		if cat.ID == "discussion-learning" && cat.QuestionCount != 1 {
			t.Fatal("published category count missing")
		}
	}
	for _, cat := range unmarshal[[]discussionCategoryView](t, c.request("GET", "/api/discussion-categories?direction=机械", nil, 200)["categories"]) {
		if cat.QuestionCount != 0 {
			t.Fatal("direction scoped count mismatch")
		}
	}
}
