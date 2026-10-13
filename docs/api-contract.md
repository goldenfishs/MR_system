# MVP API contract

JSON API under `/api`. Successful responses are JSON, errors are `{ "error": "Human-readable message" }` with appropriate HTTP status. Auth uses HttpOnly SameSite=Lax session cookie. Mutations validate Origin when present. Dates are ISO 8601 strings. IDs are strings. Empty optional strings are permitted. There is no real external notification delivery in MVP.

## Types

```ts
type Role = 'admin' | 'mentor' | 'interviewer' | 'student';
type Team = 'RoboMaster' | 'ROBOCON';
type Direction = '机械' | '电控' | '视觉算法' | '硬件' | '运营';
type ApplicantStatus = 'submitted' | 'screening' | 'interview' | 'accepted' | 'rejected';
interface User { id: string; name: string; email: string; role: Role; team: string; direction: string; joinedAt: string; }
interface Applicant {
 id: string; userId: string; name: string; email: string; phone: string; university: string;
 major: string; year: string; team: string; direction: string; secondDirection: string;
 motivation: string; experience: string; portfolioUrl: string; status: ApplicantStatus;
 interviewAt: string; interviewerId: string; score: number | null; notes: string;
 createdAt: string; updatedAt: string;
}
interface Task {
 id: string; title: string; description: string; direction: string; team: string;
 kind: 'code' | 'document' | 'hardware' | 'quiz'; difficulty: '入门' | '进阶' | '挑战';
 dueAt: string; resourcesUrl: string; maxScore: number; required: boolean;
 createdBy: string; createdAt: string; submissionCount: number;
 mySubmission: Submission | null;
}
interface Submission {
 id: string; taskId: string; userId: string; userName: string; content: string;
 repoUrl: string; artifactUrl: string; version: number;
 status: 'pending' | 'approved' | 'changes_requested'; score: number | null;
 feedback: string; reviewedBy: string; createdAt: string; reviewedAt: string;
}
interface Activity { id: string; actor: string; action: string; target: string; createdAt: string; }
interface Settings { organization: string; season: string; applicationOpen: boolean; applicationDeadline: string; hydroUrl: string; announcement: string; }
```

## Routes

- `GET /api/health` -> `{status:"ok"}`
- `POST /api/auth/login` `{email,password}` -> `{user:User}`
- `POST /api/auth/register` `{name,email,password}` -> `{user:User}`; student role only; logged in on success.
- `GET /api/auth/me` -> `{user:User,demoMode:boolean}` (401 anonymous)
- `POST /api/auth/logout` -> `{ok:true}`
- `GET /api/public/config` -> `{organization,season,demoMode,applicationOpen,applicationDeadline}` (no auth)
- `GET /api/dashboard` -> `{stats:{applicants:number,pendingReviews:number,activeTasks:number,members:number},pipeline:{status:ApplicantStatus,label:string,count:number}[],recentActivity:Activity[],upcomingInterviews:Applicant[]}`; student data scoped to own records.
- `GET /api/applicants` -> `{items:Applicant[]}`; student own, interviewer assigned, admin/mentor all. Student does not receive notes or score.
- `POST /api/applicants` -> `{item:Applicant}`; fields matching applicant form. Student creates own; admin may create. One application per user in MVP. Existing application updates only in submitted/screening state, before deadline. Applicant identity bound to session.
- `PATCH /api/applicants/:id` `{status?,interviewAt?,interviewerId?,score?,notes?}` -> `{item:Applicant}`; admin can set final status; assigned interviewer can score/notes only. Accept creates no new identity: user team/direction updated, considered member.
- `GET /api/members` -> `{items:User[]}`; staff only. Includes staff and admitted students.
- `POST /api/members` `{name,email,password,role:'mentor'|'interviewer',team,direction}` -> `{item:User}`; admin only, initial password 12–72 bytes. Creates a staff identity.
- `GET /api/tasks` -> `{items:Task[]}`; authenticated. Includes submissionCount (staff) and own latest mySubmission.
- `POST /api/tasks` fields of Task excluding server-managed fields -> `{item:Task}`; admin/mentor only.
- `GET /api/tasks/:id/submissions` -> `{items:Submission[]}`; student own, admin/mentor all.
- `POST /api/tasks/:id/submissions` `{content,repoUrl,artifactUrl}` -> `{item:Submission}`; admitted student only, matching task team/direction (empty means all), deadline enforced. Creates immutable version; old versions remain.
- `PATCH /api/submissions/:id` `{status:'approved'|'changes_requested',score:number,feedback:string}` -> `{item:Submission}`; admin/mentor only, only latest version can be reviewed; score within task.maxScore.
- `GET /api/settings` -> `{settings:Settings}`; authenticated, safe public-facing settings.
- `PATCH /api/settings` partial Settings -> `{settings:Settings}`; admin only. hydroUrl must be http(s) or empty, no secrets.

## Seeded demo identities (DEMO_MODE=true only)

`admin@demo.local` / `DemoAdmin!2026`; `mentor@demo.local` / `DemoMentor!2026`; `student@demo.local` / `DemoStudent!2026`; `interviewer@demo.local` / `DemoInterview!2026`.

Server local address `127.0.0.1:8080`; Vite proxies /api. Supports DATABASE_URL PostgreSQL; otherwise SQLite DATA_DIR, default ./data. Frontend assets served via WEB_DIR, default ../frontend/dist. Production disables demo and requires administrator bootstrap environment. SESSION_SECURE=true for HTTPS. Same-origin deployment required. No wildcard CORS.
