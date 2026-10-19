import test from 'node:test';
import assert from 'node:assert/strict';
import { algorithmGroups, algorithmCourseGroups, compareAlgorithmCourse } from '../src/lib/problem-curriculum.ts';
const p = (title: string, tags: string[], createdAt = '2026-10-18') => ({ title, tags, createdAt });
test('curriculum chapters take priority and old banks retain groups', () => {
  assert.deepEqual(algorithmGroups(p('x', ['系统学习路径', '02 分支判断', '条件'])), ['02 分支判断']);
  assert.deepEqual(algorithmGroups(p('x', ['算法阶梯', '第一阶段·编程入门'])), ['第一阶段·编程入门']);
  assert.deepEqual(algorithmGroups(p('x', ['视觉培训'])), ['课程内部练习']);
  assert.deepEqual(algorithmGroups(p('x', ['Python'])), ['基础知识与练习']);
  assert.equal(algorithmCourseGroups.length, 18);
});
test('course ordering uses chapter and numeric sequence regardless of import order', () => {
  const rows = [p('学习路径 016 · 判断', ['02 分支判断']), p('学习路径 010 · 十', ['01 输入输出与表达式']), p('学习路径 002 · 二', ['01 输入输出与表达式'])];
  assert.deepEqual([...rows].sort(compareAlgorithmCourse).map(x => x.title), ['学习路径 002 · 二', '学习路径 010 · 十', '学习路径 016 · 判断']);
  assert.equal(rows[0]!.title, '学习路径 016 · 判断');
});
test('fallback ordering is stable and legacy numbered titles sort naturally', () => {
  assert.ok(compareAlgorithmCourse(p('算法阶梯 9 · 九', ['第一阶段·编程入门']), p('算法阶梯 10 · 十', ['第一阶段·编程入门'])) < 0);
  assert.equal(compareAlgorithmCourse(p('x', []), p('x', [])), 0);
});
