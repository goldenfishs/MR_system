import type { AssignmentQuestion, Task, AssignmentQuestionType } from "./types";

export interface AssignmentQuestionInput {
  id: string;
  groupId?: string;
  groupTitle?: string;
  groupDescription?: string;
  title: string;
  description: string;
  fileIds?: string[];
  type?: AssignmentQuestionType;
  options?: string[];
  autoGrade?: boolean;
  answers?: string[];
  judgeRunId?: string;
  judge?: { enabled: boolean; testCases: unknown[]; languageIds: number[] };
  maxScore: number;
}

export function questionScoreTotal(questions: readonly AssignmentQuestionInput[]): number {
  return questions.reduce((sum, question) => sum + Number(question.maxScore || 0), 0);
}

export function questionSetError(questions: readonly AssignmentQuestionInput[]): string {
  if (!questions.length) return "请至少添加一道题目";
  if (questions.some(question => question.groupId && !question.groupTitle?.trim())) return "请填写组合大题的标题";
  if (questions.some(question =>
    !question.title.trim() || (!question.description.trim() && !question.fileIds?.length) ||
    !Number.isFinite(Number(question.maxScore)) || Number(question.maxScore) <= 0
  )) return "请为每道题填写题目名称、要求和大于 0 的分值";
  if (questions.some(question =>
    (question.type === "single_choice" || question.type === "multiple_choice") &&
    (!question.options || question.options.length < 2 || question.options.some(option => !option.trim()))
  )) return "选择题至少需要两个完整选项";
  for (const [index, question] of questions.entries()) {
    if (question.type === "code" && (!question.judge?.enabled || !question.judge.testCases.length || !question.judge.languageIds.length)) {
      return `第 ${index + 1} 题：代码题需要启用在线判题，并配置测试用例和允许语言`;
    }
  }
  for (const [index, question] of questions.entries()) {
    if (!question.autoGrade) continue;
    const prefix = `第 ${index + 1} 题：`;
    if (!["single_choice", "multiple_choice", "fill_blank"].includes(question.type || "")) return prefix + "该题型不支持自动判分";
    const answers = (question.answers || []).map(answer => answer.trim()).filter(Boolean);
    if (!answers.length) return prefix + "请填写自动判分的标准答案";
    if (new Set(answers.map(answer => answer.trim())).size !== answers.length) return prefix + "标准答案不能重复";
    if (question.type === "single_choice" && answers.length !== 1) return prefix + "单选题只能设置一个正确选项";
    if (question.type !== "fill_blank" && answers.some(answer => !/^(0|[1-9]\d*)$/.test(answer) || Number(answer) >= (question.options?.length || 0))) return prefix + "请选择有效的正确选项";
  }
  if (questionScoreTotal(questions) > 1000) return "所有题目分值合计不能超过 1000 分";
  return "";
}

type AssignmentProgressAnswer = readonly string[] | { fileIds?: readonly string[]; response?: string; judgeRunId?: string };

function isFileIdList(answer: AssignmentProgressAnswer): answer is readonly string[] {
  return Array.isArray(answer);
}

export function completedQuestionCount(
  questions: readonly Pick<AssignmentQuestionInput, "id" | "type">[],
  answers: Readonly<Record<string, AssignmentProgressAnswer>>,
): number {
  return questions.reduce((count, question) => {
    const answer = answers[question.id];
    if (!answer) return count;
    if (isFileIdList(answer)) return count + (answer.length > 0 ? 1 : 0);
    if ((question.type || "drawing") === "drawing") return count + ((answer.fileIds?.length || 0) > 0 ? 1 : 0);
    if (question.type === "short_answer") return count + (Boolean(answer.response?.trim()) || (answer.fileIds?.length || 0) > 0 ? 1 : 0);
    if (question.type === "code") return count + (Boolean(answer.judgeRunId) ? 1 : 0);
    if (question.type === "multiple_choice") {
      try { return count + ((JSON.parse(answer.response || "[]") as unknown[]).length > 0 ? 1 : 0); }
      catch { return count; }
    }
    return count + (Boolean(answer.response?.trim()) ? 1 : 0);
  }, 0);
}

/** Copy an authored snapshot rather than mutable references to the bank. */
export function questionFromProblem(task: Task, id: string): AssignmentQuestion {
  const problem = task.mechanicalProblem;
  const type = problem?.type || "code";
  const multipleBlanks = type === "fill_blank" && (problem?.blankCount || 0) > 1;
  return {
    id, sourceProblemId: task.id, title: task.title,
    illustrationUrl: task.illustrationUrl, illustrationAlt: task.illustrationAlt,
    description: task.description + (multipleBlanks ? "\n请按空的顺序逐行填写答案，每行一个。" : ""),
    type, maxScore: task.maxScore || 10, fileIds: [...(task.fileIds || [])],
    options: [...(problem?.options || [])],
    autoGrade: Boolean(problem && type !== "drawing" && problem.answers?.length),
    answers: multipleBlanks ? [(problem?.answers || []).join("\n")] : [...(problem?.answers || [])],
    explanation: problem?.explanation || "",
    judge: task.judge ? { ...task.judge, languageIds: [...task.judge.languageIds], testCases: task.judge.testCases.map(item => ({ ...item })) } : undefined,
  };
}

export function questionGroupLabel(questions: readonly AssignmentQuestionInput[], index: number): string {
  const question = questions[index];
  if (!question?.groupId) return `第 ${index + 1} 题`;
  const siblings = questions.filter(item => item.groupId === question.groupId);
  return `${question.groupTitle || "组合大题"} · 第 ${siblings.findIndex(item => item.id === question.id) + 1} 小题`;
}
