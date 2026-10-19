export const algorithmChapters = [
  '01 输入输出与表达式', '02 分支判断', '03 循环与数论入门', '04 数组与矩阵',
  '05 字符串处理', '06 函数与递归', '07 排序与二分', '08 栈队列与集合',
  '09 前缀和与双指针', '10 贪心与动态规划', '11 搜索与回溯', '12 图论与综合应用',
];
const ladderStages = ['第一阶段·编程入门', '第二阶段·数组与容器', '第三阶段·贪心与搜索', '第四阶段·动态规划'];
export const algorithmCourseGroups = [...algorithmChapters, ...ladderStages, '课程内部练习', '基础知识与练习'];
type Problem = { title: string; tags?: string[]; createdAt: string };

export function algorithmGroups(task: Pick<Problem, 'tags'>): string[] {
  const tags = task.tags || [];
  const chapters = algorithmChapters.filter(chapter => tags.includes(chapter));
  if (chapters.length) return chapters;
  const stages = ladderStages.filter(stage => tags.includes(stage));
  if (stages.length) return stages;
  return [tags.includes('视觉培训') ? '课程内部练习' : '基础知识与练习'];
}

export function compareAlgorithmCourse(a: Problem, b: Problem): number {
  const rank = (task: Problem) => algorithmCourseGroups.indexOf(algorithmGroups(task)[0]!);
  const difference = rank(a) - rank(b);
  if (difference) return difference;
  const sequence = (task: Problem) => {
    const match = task.title.match(/^学习路径[ ]+([0-9]+)[ ]*[·.]/);
    return match ? Number(match[1]) : Number.MAX_SAFE_INTEGER;
  };
  return sequence(a) - sequence(b) || a.title.localeCompare(b.title, 'zh-CN', { numeric: true }) || a.createdAt.localeCompare(b.createdAt);
}
