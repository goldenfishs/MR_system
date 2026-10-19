import test from "node:test";
import assert from "node:assert/strict";
import { completedQuestionCount, questionScoreTotal, questionSetError, questionFromProblem, questionGroupLabel } from "../src/lib/mechanical-assignment.ts";
import type { Task } from "../src/lib/types.ts";

const question = (id: string, maxScore: number) => ({ id, title: `题目 ${id}`, description: "完成并提交图纸", maxScore });

test("机械多题作业校验题目内容和总分", () => {
  assert.equal(questionSetError([]), "请至少添加一道题目");
  assert.equal(questionSetError([question("a", 20), question("b", 30)]), "");
  assert.equal(questionScoreTotal([question("a", 20), question("b", 30)]), 50);
  assert.match(questionSetError([question("a", 600), question("b", 401)]), /不能超过 1000/);
  assert.match(questionSetError([{ ...question("a", 0), title: "" }]), /题目名称、要求和大于 0/);
});

test("作业题型模板校验选择题选项", () => {
  const choice = { ...question("choice", 5), type: "single_choice" as const, options: ["选项 A", ""] };
  assert.match(questionSetError([choice]), /至少需要两个完整选项/);
  assert.equal(questionSetError([{ ...choice, options: ["选项 A", "选项 B"] }]), "");
  assert.equal(questionSetError([{ ...question("short", 5), type: "short_answer" }]), "");
});

test("逐题进度按题型识别文件或文字答案", () => {
  const questions = [question("a", 10), question("b", 10), question("c", 10)];
  assert.equal(completedQuestionCount(questions, { a: ["file-1"], b: [], c: ["file-3", "file-4"] }), 2);
  assert.equal(completedQuestionCount([
    { id: "choice", type: "single_choice" },
    { id: "essay", type: "short_answer" },
    { id: "drawing", type: "drawing" },
  ], {
    choice: { response: "0" },
    essay: { response: "" },
    drawing: { fileIds: ["file-1"] },
  }), 2);
});


test("自动判分校验标准答案，保留旧题人工评阅", () => {
  const base = { ...question("auto", 10), type: "single_choice" as const, options: ["甲", "乙"] };
  assert.equal(questionSetError([base]), "");
  assert.match(questionSetError([{ ...base, autoGrade: true }]), /标准答案/);
  assert.equal(questionSetError([{ ...base, autoGrade: true, answers: ["0"] }]), "");
  assert.match(questionSetError([{ ...base, autoGrade: true, answers: ["0", "1"] }]), /只能设置一个/);
  assert.match(questionSetError([{ ...base, autoGrade: true, answers: ["2"] }]), /有效的正确选项/);
  assert.match(questionSetError([{ ...base, type: "multiple_choice", autoGrade: true, answers: ["1", "1"] }]), /不能重复/);
  assert.equal(questionSetError([{ ...base, type: "fill_blank", autoGrade: true, answers: [" Answer ", "answer", ""] }]), "");
  assert.match(questionSetError([{ ...base, type: "fill_blank", autoGrade: true, answers: [" "] }]), /标准答案/);
  assert.match(questionSetError([{ ...base, type: "drawing", autoGrade: true, answers: ["0"] }]), /不支持自动判分/);
});


test("纯题图无需重复输入题干，填空允许多行完整答案", () => {
  assert.equal(questionSetError([{ ...question("image", 10), description: "", fileIds: ["image-file"] }]), "");
  assert.match(questionSetError([{ ...question("empty", 10), description: "", fileIds: [] }]), /题目名称、要求/);
  assert.equal(questionSetError([{ ...question("output", 10), type: "fill_blank", autoGrade: true, answers: ["hello\nworld", "hello\nworld\n!"] }]), "");
});


test("简答题可仅交文件，填空题仍需文字答案", () => {
  const questions = [{ id: "code", type: "short_answer" as const }, { id: "blank", type: "fill_blank" as const }];
  assert.equal(completedQuestionCount(questions, { code: { response: "  ", fileIds: ["main-c"] }, blank: { response: " ", fileIds: ["wrong-file"] } }), 1);
  assert.equal(completedQuestionCount(questions, { code: { response: "text", fileIds: [] }, blank: { response: "0" } }), 2);
  assert.equal(completedQuestionCount(questions, { code: { response: "  ", fileIds: [] } }), 0);
});

test("在线代码题必须设置评测并以判题记录计入进度", () => {
  const codeQuestion = { ...question("code", 20), type: "code" as const };
  assert.match(questionSetError([codeQuestion]), /代码题需要启用在线判题/);
  const configured = {
    ...codeQuestion,
    judge: { enabled: true, languageIds: [50], testCases: [{ input: "1", expectedOutput: "1" }] },
  };
  assert.equal(questionSetError([configured]), "");
  assert.equal(completedQuestionCount([{ id: "code", type: "code" }], {}), 0);
  assert.equal(completedQuestionCount([{ id: "code", type: "code" }], { code: { judgeRunId: "run-1" } }), 1);
});

test("题库选题复制独立快照，保留来源及隐藏测试用例", () => {
  const source = { id: "bank-1", title: "数组", description: "计算", fileIds: ["figure"], maxScore: 20, illustrationUrl: "/question-bank/demo.svg", judge: { enabled: true, languageIds: [54], testCases: [{ input: "1", expectedOutput: "2", hidden: true }], timeLimitSeconds: 2, totalCases: 1 } } as Task;
  const copy = questionFromProblem(source, "question-1");
  assert.equal(copy.sourceProblemId, "bank-1");
  assert.equal(copy.illustrationUrl, source.illustrationUrl);
  assert.equal(copy.type, "code");
  assert.equal(copy.judge?.testCases[0]?.hidden, true);
  copy.fileIds.push("another");
  copy.judge!.testCases[0]!.expectedOutput = "changed";
  copy.judge!.languageIds.push(71);
  assert.deepEqual(source.fileIds, ["figure"]);
  assert.equal(source.judge!.testCases[0]!.expectedOutput, "2");
  assert.deepEqual(source.judge!.languageIds, [54]);
});

test("题库多空答案转换为作业完整答案，选项和答案互相独立", () => {
  const source = { id: "fill", title: "两空", description: "填空", maxScore: 10, mechanicalProblem: { type: "fill_blank", blankCount: 2, answers: ["int", "float"], options: [], answerFileIds: [], explanation: "说明" } } as Task;
  const copy = questionFromProblem(source, "new");
  assert.equal(copy.autoGrade, true);
  assert.deepEqual(copy.answers, ["int\nfloat"]);
  assert.match(copy.description, /逐行填写/);
  assert.equal(questionSetError([copy]), "");
});

test("组合大题按独立小题计分，标题必须填写", () => {
  const questions = [{ ...question("a", 20), groupId: "g", groupTitle: "路径规划", type: "short_answer" as const }, { ...question("b", 30), groupId: "g", groupTitle: "路径规划", type: "drawing" as const }];
  assert.equal(questionScoreTotal(questions), 50);
  assert.equal(questionGroupLabel(questions, 1), "路径规划 · 第 2 小题");
  assert.equal(questionSetError(questions), "");
  assert.match(questionSetError([{ ...questions[0]!, groupTitle: " " }]), /大题的标题/);
  assert.equal(completedQuestionCount(questions, { a: { response: "说明" }, b: { fileIds: ["drawing"] } }), 2);
});
