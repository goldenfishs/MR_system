import test from 'node:test';
import assert from 'node:assert/strict';
import { algorithmGroups, algorithmCourseGroups, compareAlgorithmCourse, problemCourses, problemCourse, problemTopics } from '../src/lib/problem-curriculum.ts';
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
test('course categories remain bounded and separate courses from fine-grained tags', () => {
  const rows = [
    { direction: '电控', tags: ['C语言进阶', '指针', '生命周期与释放'] },
    { direction: '视觉算法', tags: ['C++', '系统学习路径', '07 排序与二分', '下标与值的互换'] },
    { direction: '视觉算法', tags: ['Python', '字符串'] },
    { direction: '电控', tags: ['STM32单片机基础', 'GPIO', '上拉电阻'] },
    { direction: '硬件', tags: ['MOSFET', '驱动电路'] },
    { direction: '机械', tags: ['投影', '三视图'] },
    { direction: '电控', tags: ['自定义知识点'] },
  ];
  assert.deepEqual(rows.map(problemCourse), ['C语言进阶', '数据结构与算法', 'Python基础', 'STM32单片机基础', '硬件与电路', '机械与制图', '其他练习']);
  assert.ok(rows.every(row => problemCourses.includes(problemCourse(row))));
  assert.equal(problemCourses.length, 10);
  assert.equal(problemCourse({ tags: ['算法阶梯', '第三阶段·贪心与搜索', 'Python'] }), '数据结构与算法');
  assert.equal(problemCourse({ tags: ['视觉培训', '函数'] }), 'C++基础');
});
test('topic filtering removes metadata while retaining unique searchable details without changing source tags', () => {
  const row = { tags: ['C++', '系统学习路径', '07 排序与二分', '二分查找', '二分查找', '边界处理'] };
  const original = [...row.tags];
  assert.deepEqual(problemTopics(row), ['二分查找', '边界处理']);
  assert.deepEqual(row.tags, original);
  assert.deepEqual(problemTopics({}), []);
});
