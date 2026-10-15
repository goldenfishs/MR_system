import type { JudgeConfig } from "./platform";
import type { MechanicalProblem } from "./learning";
export type Role = "admin" | "group_admin" | "member" | "reserve" | "freshman";
export type MemberType = "formal" | "reserve" | "freshman";
export type Permission = "recruitment" | "assessment" | "tasks" | "knowledge" | "discussions" | "members" | "records" | "mail";
export interface GroupGrant { direction: Direction; permissions: Permission[]; }
export type Team = "RoboMaster" | "RoboCon" | "ROBOCON";

export interface DiscussionReply {
  id: string;
  content: string;
  userId: string;
  userName: string;
  createdAt: string;
}
export interface Discussion {
  id: string;
  title: string;
  content: string;
  direction: string;
  status: "open" | "resolved";
  published: boolean;
  createdBy: string;
  authorName: string;
  createdAt: string;
  updatedAt: string;
  replies: DiscussionReply[];
}
export type Direction = "机械" | "电控" | "视觉算法" | "硬件" | "运营";
export type ApplicantStatus =
  "submitted" | "screening" | "interview" | "accepted" | "rejected";
export interface User {
  id: string;
  name: string;
  email: string;
  role: Role;
  memberType?: MemberType;
  assessmentPassedAt?: string;
  assessmentConfirmedBy?: string;
  groupGrants?: GroupGrant[];
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
  assigned?: boolean;
  canSubmit?: boolean;
  category?: "assignment" | "exam" | "problem";
  tags?: string[];
  acceptedCount?: number;
  attemptCount?: number;
  locked?: boolean;
  myAccepted?: boolean;
  teamId?: string;
  fileIds?: string[];
  submissionType?: "" | "files" | "code" | "project";
  startsAt?: string;
  judge?: JudgeConfig;
  mechanicalProblem?: MechanicalProblem;
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
  /** Optional branding fields supplied by newer servers. */
  brandName?: string;
  logoUrl?: string;
  teamName?: string;
  secondaryTeamName?: string;
  competitions?: string[];
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
  brandName?: string;
  logoUrl?: string;
  teamName?: string;
  secondaryTeamName?: string;
  competitions?: string[];
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
