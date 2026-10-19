package main

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
)

type DiscussionCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Direction   string `json:"direction"`
	SortOrder   int    `json:"sortOrder"`
	Archived    bool   `json:"archived"`
}
type discussionCategorySettings struct {
	Categories []DiscussionCategory `json:"categories"`
}
type discussionCategoryView struct {
	DiscussionCategory
	QuestionCount int  `json:"questionCount"`
	CanManage     bool `json:"canManage"`
}

func (a *App) registerDiscussionCategoryRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/discussion-categories", a.wrap(true, a.listDiscussionCategories))
	m.HandleFunc("POST /api/discussion-categories", a.wrap(true, a.createDiscussionCategory))
	m.HandleFunc("PATCH /api/discussion-categories/{id}", a.wrap(true, a.updateDiscussionCategory))
	m.HandleFunc("DELETE /api/discussion-categories/{id}", a.wrap(true, a.deleteDiscussionCategory))
}
func (a *App) discussionCategories() ([]DiscussionCategory, error) {
	cfg, err := readOne[discussionCategorySettings](a.store, "SELECT payload FROM settings WHERE id='discussion-categories'")
	if errors.Is(err, sql.ErrNoRows) {
		return []DiscussionCategory{
			{ID: "discussion-learning", Name: "学习答疑", Description: "课程知识、基础概念与学习方法", SortOrder: 10},
			{ID: "discussion-assignments", Name: "作业问题", Description: "题目理解、提交方式与作业反馈", SortOrder: 20},
			{ID: "discussion-debugging", Name: "技术排错", Description: "程序报错、设备调试与故障排查", SortOrder: 30},
			{ID: "discussion-community", Name: "站务与建议", Description: "平台使用问题与改进建议", SortOrder: 40},
		}, nil
	}
	if cfg.Categories == nil {
		cfg.Categories = []DiscussionCategory{}
	}
	return cfg.Categories, err
}
func (a *App) saveDiscussionCategories(items []DiscussionCategory) error {
	return a.store.exec("INSERT INTO settings(id,payload) VALUES('discussion-categories',?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", encoded(discussionCategorySettings{items}))
}
func validateDiscussionCategory(c DiscussionCategory, items []DiscussionCategory) error {
	if c.Name == "" || utf8.RuneCountInString(c.Name) > 30 || strings.ContainsAny(c.Name, "\r\n\t") || utf8.RuneCountInString(c.Description) > 200 || c.SortOrder < 0 || c.SortOrder > 9999 || c.Direction != "" && !validDirection(c.Direction) {
		return fail(400, "分类名称需为 1–30 字，说明不超过 200 字，排序为 0–9999，并选择有效组别")
	}
	if c.Name == "未分类" || c.Name == "全部分类" {
		return fail(400, "请使用其他分类名称，未分类和全部分类为系统筛选项")
	}
	for _, item := range items {
		if item.ID != c.ID && item.Direction == c.Direction && strings.EqualFold(item.Name, c.Name) {
			return fail(409, "该范围已有同名分类，请编辑或恢复原分类")
		}
	}
	return nil
}
func (a *App) validateDiscussionCategoryID(id, direction string) error {
	if id == "" {
		return nil
	}
	items, err := a.discussionCategories()
	if err != nil {
		return err
	}
	for _, c := range items {
		if c.ID == id {
			if c.Archived {
				return fail(400, "该分类已停用，请选择其他分类")
			}
			if c.Direction != "" && c.Direction != direction {
				return fail(400, "分类所属组别与问题指定组别不一致")
			}
			return nil
		}
	}
	return fail(400, "问题分类不存在，请刷新后重新选择")
}
func (a *App) listDiscussionCategories(w http.ResponseWriter, r *http.Request, u User) error {
	items, err := a.discussionCategories()
	if err != nil {
		return err
	}
	discussions, err := readAll[Discussion](a.store, "SELECT payload FROM discussions")
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, d := range discussions {
		if discussionVisible(d, u) && (r.URL.Query().Get("direction") == "" || d.Direction == r.URL.Query().Get("direction")) {
			counts[d.CategoryID]++
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Archived != items[j].Archived {
			return !items[i].Archived
		}
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	})
	result := []discussionCategoryView{}
	for _, c := range items {
		result = append(result, discussionCategoryView{c, counts[c.ID], hasPermission(u, c.Direction, "discussions")})
	}
	respond(w, 200, map[string]any{"categories": result})
	return nil
}
func (a *App) createDiscussionCategory(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "discussions") {
		return fail(403, "没有讨论分类管理权限")
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Direction   string `json:"direction"`
		SortOrder   int    `json:"sortOrder"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := requirePermission(u, in.Direction, "discussions"); err != nil {
		return err
	}
	items, err := a.discussionCategories()
	if err != nil {
		return err
	}
	if len(items) >= 200 {
		return fail(409, "分类数量已达 200 个，请先整理现有分类")
	}
	c := DiscussionCategory{ID: randomID(), Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description), Direction: in.Direction, SortOrder: in.SortOrder}
	if err = validateDiscussionCategory(c, items); err != nil {
		return err
	}
	if err = a.saveDiscussionCategories(append(items, c)); err != nil {
		return err
	}
	a.activity(u, "创建讨论分类", c.Name)
	respond(w, 201, map[string]any{"category": discussionCategoryView{DiscussionCategory: c, CanManage: true}})
	return nil
}
func (a *App) updateDiscussionCategory(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "discussions") {
		return fail(403, "没有讨论分类管理权限")
	}
	items, err := a.discussionCategories()
	if err != nil {
		return err
	}
	index := -1
	for i, c := range items {
		if c.ID == r.PathValue("id") {
			index = i
			break
		}
	}
	if index < 0 {
		return fail(404, "分类不存在")
	}
	c := items[index]
	if err = requirePermission(u, c.Direction, "discussions"); err != nil {
		return err
	}
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Direction   *string `json:"direction"`
		SortOrder   *int    `json:"sortOrder"`
		Archived    *bool   `json:"archived"`
	}
	if err = decode(r, &in); err != nil {
		return err
	}
	if in.Direction != nil && *in.Direction != c.Direction {
		return fail(400, "分类创建后不能更改所属范围，请新建分类再整理问题")
	}
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		c.Description = strings.TrimSpace(*in.Description)
	}
	if in.SortOrder != nil {
		c.SortOrder = *in.SortOrder
	}
	if in.Archived != nil {
		c.Archived = *in.Archived
	}
	if err = validateDiscussionCategory(c, items); err != nil {
		return err
	}
	items[index] = c
	if err = a.saveDiscussionCategories(items); err != nil {
		return err
	}
	a.activity(u, "更新讨论分类", c.Name)
	respond(w, 200, map[string]any{"category": c})
	return nil
}
func (a *App) deleteDiscussionCategory(w http.ResponseWriter, r *http.Request, u User) error {
	if !hasAnyPermission(u, "discussions") {
		return fail(403, "没有讨论分类管理权限")
	}
	items, err := a.discussionCategories()
	if err != nil {
		return err
	}
	index := -1
	for i, c := range items {
		if c.ID == r.PathValue("id") {
			index = i
			break
		}
	}
	if index < 0 {
		return fail(404, "分类不存在")
	}
	c := items[index]
	if err = requirePermission(u, c.Direction, "discussions"); err != nil {
		return err
	}
	discussions, err := readAll[Discussion](a.store, "SELECT payload FROM discussions")
	if err != nil {
		return err
	}
	for _, d := range discussions {
		if d.CategoryID == c.ID {
			return fail(409, "分类下仍有问题，请先移到其他分类，或停用分类保留历史问题")
		}
	}
	items = append(items[:index], items[index+1:]...)
	if err = a.saveDiscussionCategories(items); err != nil {
		return err
	}
	a.activity(u, "删除空讨论分类", c.Name)
	respond(w, 200, map[string]bool{"ok": true})
	return nil
}
