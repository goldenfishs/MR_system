package main

type User struct {
	ID                    string       `json:"id"`
	Name                  string       `json:"name"`
	Email                 string       `json:"email"`
	Role                  string       `json:"role"`
	MemberType            string       `json:"memberType"`
	AssessmentPassedAt    string       `json:"assessmentPassedAt,omitempty"`
	AssessmentConfirmedBy string       `json:"assessmentConfirmedBy,omitempty"`
	AssessmentNote        string       `json:"assessmentNote,omitempty"`
	GroupGrants           []GroupGrant `json:"groupGrants"`
	Team                  string       `json:"team"`
	Direction             string       `json:"direction"`
	JoinedAt              string       `json:"joinedAt"`
	EmailVerifiedAt       string       `json:"emailVerifiedAt,omitempty"`
}
type Applicant struct {
	ID              string   `json:"id"`
	UserID          string   `json:"userId"`
	Name            string   `json:"name"`
	Email           string   `json:"email"`
	Phone           string   `json:"phone"`
	University      string   `json:"university"`
	Major           string   `json:"major"`
	Year            string   `json:"year"`
	Team            string   `json:"team"`
	Direction       string   `json:"direction"`
	SecondDirection string   `json:"secondDirection"`
	Motivation      string   `json:"motivation"`
	Experience      string   `json:"experience"`
	PortfolioURL    string   `json:"portfolioUrl"`
	Status          string   `json:"status"`
	InterviewAt     string   `json:"interviewAt"`
	InterviewerID   string   `json:"interviewerId"`
	Score           *float64 `json:"score"`
	Notes           string   `json:"notes"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt"`
}
type Task struct {
	Assigned        bool         `json:"assigned"`
	CanSubmit       bool         `json:"canSubmit"`
	Category        string       `json:"category"`
	Tags            []string     `json:"tags"`
	MyAccepted      bool         `json:"myAccepted"`
	AcceptedCount   int          `json:"acceptedCount"`
	AttemptCount    int          `json:"attemptCount"`
	Locked          bool         `json:"locked"`
	TeamID          string       `json:"teamId,omitempty"`
	FileIDs         []string     `json:"fileIds"`
	SubmissionType  string       `json:"submissionType,omitempty"`
	StartsAt        string       `json:"startsAt,omitempty"`
	Judge           *JudgeConfig `json:"judge,omitempty"`
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Direction       string       `json:"direction"`
	Team            string       `json:"team"`
	Kind            string       `json:"kind"`
	Difficulty      string       `json:"difficulty"`
	DueAt           string       `json:"dueAt"`
	ResourcesURL    string       `json:"resourcesUrl"`
	MaxScore        float64      `json:"maxScore"`
	Required        bool         `json:"required"`
	CreatedBy       string       `json:"createdBy"`
	CreatedAt       string       `json:"createdAt"`
	SubmissionCount int          `json:"submissionCount"`
	MySubmission    *Submission  `json:"mySubmission"`
}
type Submission struct {
	FileIDs     []string `json:"fileIds"`
	JudgeRunID  string   `json:"judgeRunId,omitempty"`
	ID          string   `json:"id"`
	TaskID      string   `json:"taskId"`
	UserID      string   `json:"userId"`
	UserName    string   `json:"userName"`
	Content     string   `json:"content"`
	RepoURL     string   `json:"repoUrl"`
	ArtifactURL string   `json:"artifactUrl"`
	Version     int      `json:"version"`
	Status      string   `json:"status"`
	Score       *float64 `json:"score"`
	Feedback    string   `json:"feedback"`
	ReviewedBy  string   `json:"reviewedBy"`
	CreatedAt   string   `json:"createdAt"`
	ReviewedAt  string   `json:"reviewedAt"`
}
type Activity struct {
	ID        string `json:"id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	CreatedAt string `json:"createdAt"`
	UserID    string `json:"-"`
}
type Settings struct {
	BrandName           string   `json:"brandName"`
	LogoURL             string   `json:"logoUrl"`
	TeamName            string   `json:"teamName"`
	SecondaryTeamName   string   `json:"secondaryTeamName"`
	Competitions        []string `json:"competitions"`
	Organization        string   `json:"organization"`
	Season              string   `json:"season"`
	ApplicationOpen     bool     `json:"applicationOpen"`
	ApplicationDeadline string   `json:"applicationDeadline"`
	HydroURL            string   `json:"hydroUrl"`
	Announcement        string   `json:"announcement"`
}
