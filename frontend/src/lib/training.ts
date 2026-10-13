import type { Direction, MemberType } from "./types";

export interface TrainingStage {
  id: string;
  title: string;
  description: string;
  order: number;
  final: boolean;
}
export interface TrainingPlan {
  id: string;
  teamId: string;
  direction: Direction;
  title: string;
  stages: TrainingStage[];
  createdBy: string;
  createdAt: string;
  updatedBy: string;
  updatedAt: string;
}
export interface TrainingDecision {
  stageId: string;
  stageTitle: string;
  status: "passed" | "eliminated";
  note: string;
  decidedBy: string;
  decidedByName: string;
  decidedAt: string;
  memberType?: MemberType;
}
export interface TrainingProgress {
  id: string;
  teamId: string;
  userId: string;
  userName: string;
  email: string;
  direction: Direction;
  cycle: number;
  planId: string;
  planTitle: string;
  stages: TrainingStage[];
  currentStageId: string;
  status: "training" | "completed" | "eliminated";
  decisions: TrainingDecision[];
  createdAt: string;
  updatedAt: string;
}
export const trainingStatusNames = { training: "培养中", completed: "考核通过", eliminated: "已淘汰" };
export const trainingStatusColors = { training: "primary", completed: "success", eliminated: "error" };
export const currentTrainingStage = (progress: TrainingProgress) => progress.stages.find(stage => stage.id === progress.currentStageId);
export const defaultTrainingStages = () => [
  { id: "", title: "基础学习", description: "完成基础知识学习与入门练习。", order: 0, final: false },
  { id: "", title: "阶段考核", description: "检验阶段学习与实践成果。", order: 1, final: false },
  { id: "", title: "最终考核", description: "综合考核通过后确认成员身份。", order: 2, final: true },
];
