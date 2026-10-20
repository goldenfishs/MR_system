export const algorithmChapters = [
  '01 输入输出与表达式', '02 分支判断', '03 循环与数论入门', '04 数组与矩阵',
  '05 字符串处理', '06 函数与递归', '07 排序与二分', '08 栈队列与集合',
  '09 前缀和与双指针', '10 贪心与动态规划', '11 搜索与回溯', '12 图论与综合应用',
];
const ladderStages = ['第一阶段·编程入门', '第二阶段·数组与容器', '第三阶段·贪心与搜索', '第四阶段·动态规划'];
export const algorithmCourseGroups = [...algorithmChapters, ...ladderStages, '课程内部练习', '基础知识与练习'];
export const problemCourses = [
  'C语言基础', 'C语言进阶', 'C++基础', 'Python基础', '数据结构与算法',
  'STM32单片机基础', '单片机通用基础', '硬件与电路', '机械与制图', '其他练习',
];
type CategorizedProblem = { direction?: string; tags?: string[] };

// Presentation categories only: preserve the original tags and question data.
export function problemCourse(task: CategorizedProblem): string {
  const tags = task.tags || [];
  if (task.direction === '机械') return '机械与制图';
  for (const course of ['C语言基础', 'C语言进阶', 'STM32单片机基础', '单片机通用基础']) {
    if (tags.includes(course)) return course;
  }
  if (task.direction === '硬件') return '硬件与电路';
  if (tags.some(tag => algorithmChapters.includes(tag) || ladderStages.includes(tag) || ['算法', '算法阶梯', '系统学习路径'].includes(tag))) return '数据结构与算法';
  if (tags.includes('Python')) return 'Python基础';
  if (tags.includes('C++') || tags.includes('视觉培训')) return 'C++基础';
  if (tags.includes('C语言')) return 'C语言基础';
  return '其他练习';
}

export function problemTopics(task: CategorizedProblem): string[] {
  const categoryTags = new Set([...problemCourses, ...algorithmCourseGroups, 'C++', 'Python', 'C语言', '系统学习路径', '算法阶梯', '视觉培训', '算法']);
  return [...new Set(task.tags || [])].filter(tag => !categoryTags.has(tag));
}
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
