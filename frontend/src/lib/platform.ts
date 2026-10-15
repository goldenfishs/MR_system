import type { User } from "./types";
export interface PhotoInput {
  name: string;
  contentBase64: string;
}
export interface ProfileInput {
  name: string;
  gender: string;
  university: string;
  major: string;
  className: string;
  studentNumber: string;
  phone: string;
  hobbies: string;
}
export interface Profile extends ProfileInput {
  userId: string;
  /** 个人头像，用于账号展示；旧账号可能没有该字段。 */
  avatarFileId?: string;
  /** 本人证件照，用于资料完整性和报名审核。 */
  photoFileId: string;
  updatedAt: string;
  complete: boolean;
}
export interface FileAsset {
  id: string;
  ownerId: string;
  name: string;
  mime: string;
  size: number;
  createdAt: string;
}
export interface TeamSpace {
  id: string;
  name: string;
  description: string;
  competition: string;
  competitions?: string[];
  directions: string[];
  status: "open" | "closed";
  deadline: string;
  createdAt: string;
  createdBy: string;
  memberCount: number;
  myApplication?: TeamApplication | null;
}
export interface TeamApplication {
  id: string;
  teamId: string;
  userId: string;
  userName: string;
  direction: string;
  resumeFileId: string;
  motivation: string;
  status: "pending" | "accepted" | "rejected";
  feedback: string;
  createdAt: string;
  updatedAt: string;
  profile?: Profile;
  trainingStatus?: "training" | "completed" | "eliminated";
  trainingStageTitle?: string;
  trainingCycle?: number;
}
export interface Membership {
  teamId: string;
  userId: string;
  direction: string;
  joinedAt: string;
}
export interface UserWithProfile extends User {
  profile?: Profile;
  /** Active groups this operator may issue a certificate for. */
  eligibleDirections?: string[];
}
export interface PersonalRecord {
  id: string;
  userId: string;
  userName: string;
  title: string;
  kind: "certificate" | "award" | "notice";
  description: string;
  fileIds: string[];
  issuedAt: string;
  createdAt: string;
  createdBy: string;
}
export interface MailRecipient {
  userId: string;
  name?: string;
  userName?: string;
  email: string;
  status: string;
  error?: string;
  sentAt?: string;
}
export interface MailCampaign {
  id: string;
  subject: string;
  body: string;
  status: string;
  recipients: MailRecipient[];
  createdAt: string;
  sentCount?: number;
  failedCount?: number;
}
export interface JudgeCase {
  input: string;
  expectedOutput: string;
  hidden: boolean;
}
export interface JudgeConfig {
  enabled: boolean;
  testCases: JudgeCase[];
  languageIds: number[];
  timeLimitSeconds: number;
  totalCases?: number;
}
export interface JudgeRun {
  id: string;
  taskId: string;
  userId: string;
  sourceFileId: string;
  languageId: number;
  status: "queued" | "running" | "completed" | "error";
  passed: number;
  total: number;
  score: number;
  results: {
    index: number;
    status: string;
    stdout: string;
    stderr: string;
    compileOutput: string;
    time: string;
    memory: number;
    hidden: boolean;
  }[];
  createdAt: string;
  error?: string;
}
export interface JudgeService {
  configured: boolean;
  languages: { id: number; name: string }[];
}
export const directions = ["机械", "电控", "视觉算法", "硬件", "运营"];
export const blankProfile = (): ProfileInput => ({
  name: "",
  gender: "",
  university: "",
  major: "",
  className: "",
  studentNumber: "",
  phone: "",
  hobbies: "",
});
export function profileError(profile: ProfileInput, hasPhoto: boolean): string {
  if (
    [
      "name",
      "gender",
      "university",
      "major",
      "className",
      "studentNumber",
      "phone",
    ].some((key) => !String(profile[key as keyof ProfileInput]).trim())
  )
    return "请填写姓名、性别、学校、专业、班级、学号和联系电话。";
  if (!hasPhoto) return "请上传本人证件照（JPEG 或 PNG，最多 2 MB）。";
  return "";
}
export const fileSize = (bytes: number) =>
  bytes >= 1048576
    ? `${(bytes / 1048576).toFixed(1)} MB`
    : `${Math.max(1, Math.ceil(bytes / 1024))} KB`;
export const dateTime = (value?: string) =>
  value ? new Date(value).toLocaleString("zh-CN") : "未设置";
export const downloadUrl = (id: string) =>
  `/api/files/${encodeURIComponent(id)}/download`;
export const previewUrl = (id: string) =>
  `/api/files/${encodeURIComponent(id)}/preview`;
