import type { DiscussionCategory } from "./types";

export const uncategorizedDiscussion = "__uncategorized";

export function categoryMatchesDirection(category: DiscussionCategory, direction: string) {
  return !category.direction || category.direction === direction;
}

export function sortDiscussionCategories(categories: DiscussionCategory[]) {
  return [...categories].sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name, "zh-CN") || a.id.localeCompare(b.id));
}

export function selectableDiscussionCategories(categories: DiscussionCategory[], direction: string, currentId = "") {
  return sortDiscussionCategories(categories.filter(category =>
    categoryMatchesDirection(category, direction) && (!category.archived || category.id === currentId),
  ));
}

export function validDiscussionCategoryId(categories: DiscussionCategory[], direction: string, id: string) {
  return categories.some(category => category.id === id && !category.archived && categoryMatchesDirection(category, direction)) ? id : "";
}

export function discussionCategoryName(categories: DiscussionCategory[], id?: string) {
  if (!id) return "未分类";
  const category = categories.find(item => item.id === id);
  return category ? `${category.name}${category.archived ? "（已停用）" : ""}` : "未分类";
}
