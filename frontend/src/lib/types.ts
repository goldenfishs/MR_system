import type { JudgeConfig } from "./platform";
export type Role = "admin" | "mentor" | "interviewer" | "student";
export type Team = "RoboMaster" | "ROBOCON";
export type Direction = "机械" | "电控" | "视觉算法" | "硬件" | "运营";
export type ApplicantStatus =
  "submitted" | "screening" | "interview" | "accepted" | "rejected";
export interface User {
  id: string;
  name: string;
  email: string;
  role: Role;
  team: string;
  direction: string;
  joinedAt: string;
  emailVerifiedAt?: string;
}
export interface Applicant {
  id: string;
  userId: string;
  name: string;
  email: string;
  phone: string;
  university: string;
  major: string;
  year: string;
  team: string;
  direction: string;
  secondDirection: string;
  motivation: string;
  experience: string;
  portfolioUrl: string;
  status: ApplicantStatus;
  interviewAt: string;
  interviewerId: string;
  score: number | null;
  notes: string;
  createdAt: string;
  updatedAt: string;
}
export interface Task {
  teamId?: string;
  fileIds?: string[];
  submissionType?: "files" | "code" | "project";
  startsAt?: string;
  judge?: JudgeConfig;
  id: string;
  title: string;
  description: string;
  direction: string;
  team: string;
  kind: "code" | "document" | "hardware" | "quiz";
  difficulty: "入门" | "进阶" | "挑战";
  dueAt: string;
  resourcesUrl: string;
  maxScore: number;
  required: boolean;
  createdBy: string;
  createdAt: string;
  submissionCount: number;
  mySubmission: Submission | null;
}
export interface Submission {
  fileIds?: string[];
  judgeRunId?: string;
  id: string;
  taskId: string;
  userId: string;
  userName: string;
  content: string;
  repoUrl: string;
  artifactUrl: string;
  version: number;
  status: "pending" | "approved" | "changes_requested";
  score: number | null;
  feedback: string;
  reviewedBy: string;
  createdAt: string;
  reviewedAt: string;
}
export interface Activity {
  id: string;
  actor: string;
  action: string;
  target: string;
  createdAt: string;
}
export interface Settings {
  organization: string;
  season: string;
  applicationOpen: boolean;
  applicationDeadline: string;
  hydroUrl: string;
  announcement: string;
}
export interface Dashboard {
  stats: {
    applicants: number;
    pendingReviews: number;
    activeTasks: number;
    members: number;
  };
  pipeline: { status: ApplicantStatus; label: string; count: number }[];
  recentActivity: Activity[];
  upcomingInterviews: Applicant[];
}
export interface PublicConfig {
  organization: string;
  season: string;
  demoMode: boolean;
  applicationOpen: boolean;
  applicationDeadline: string;
  emailVerificationRequired: boolean;
  registrationEnabled: boolean;
}
export interface EmailServiceStatus {
  configured: boolean;
  from: string;
  security: string;
}
