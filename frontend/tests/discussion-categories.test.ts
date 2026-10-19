import test from 'node:test';
import assert from 'node:assert/strict';
import { discussionCategoryName, selectableDiscussionCategories, sortDiscussionCategories, validDiscussionCategoryId } from '../src/lib/discussion-categories.ts';
import type { DiscussionCategory } from '../src/lib/types.ts';

const category = (id: string, direction = '', archived = false, sortOrder = 0): DiscussionCategory => ({ id, name: id, description: '', direction, archived, sortOrder, questionCount: 0, canManage: false });
const categories = [category('global'), category('electrical', '电控'), category('mechanical', '机械'), category('archived-global', '', true), category('archived-electrical', '电控', true)];

test('new questions offer active common categories plus their chosen group', () => {
  assert.deepEqual(selectableDiscussionCategories(categories, '电控').map(item => item.id), ['electrical', 'global']);
  assert.deepEqual(selectableDiscussionCategories(categories, '').map(item => item.id), ['global']);
});

test('changing question group clears incompatible and archived choices', () => {
  assert.equal(validDiscussionCategoryId(categories, '机械', 'electrical'), '');
  assert.equal(validDiscussionCategoryId(categories, '机械', 'global'), 'global');
  assert.equal(validDiscussionCategoryId(categories, '电控', 'archived-electrical'), '');
  assert.equal(validDiscussionCategoryId(categories, '电控', 'missing'), '');
});

test('existing archived category stays visible but unrelated archives cannot be selected', () => {
  assert.deepEqual(selectableDiscussionCategories(categories, '电控', 'archived-electrical').map(item => item.id), ['archived-electrical', 'electrical', 'global']);
  assert.deepEqual(selectableDiscussionCategories(categories, '机械', 'archived-electrical').map(item => item.id), ['global', 'mechanical']);
  assert.equal(discussionCategoryName(categories, 'archived-electrical'), 'archived-electrical（已停用）');
  assert.equal(discussionCategoryName(categories, ''), '未分类');
});

test('ordering honors sortOrder without mutating server data', () => {
  const input = [category('later', '', false, 20), category('first', '', false, 5)];
  assert.deepEqual(sortDiscussionCategories(input).map(item => item.id), ['first', 'later']);
  assert.deepEqual(input.map(item => item.id), ['later', 'first']);
});
