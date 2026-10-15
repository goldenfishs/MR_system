export interface KnowledgeResource {
  id: string;
  title: string;
  description: string;
  direction: string;
  kind: "file" | "pdf" | "video";
  fileIds: string[];
  url: string;
  createdBy: string;
  createdAt: string;
}

export interface MechanicalProblem {
  type: "single_choice" | "multiple_choice" | "fill_blank" | "drawing";
  options: string[];
  /** Choice answers are zero-based option indices; fill answers follow blank order. */
  answers: string[];
  explanation: string;
  answerFileIds: string[];
  blankCount: number;
}

export interface MechanicalPracticeResult {
  correct: boolean;
  score: number;
  maxScore: number;
  answers: string[];
  explanation: string;
}
