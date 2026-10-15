package main

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// MechanicalProblem keeps its original JSON name for compatibility. Objective
// questions are also available in the electrical, algorithm and hardware banks.
type MechanicalProblem struct {
	Type          string   `json:"type"`
	Options       []string `json:"options"`
	Answers       []string `json:"answers"`
	Explanation   string   `json:"explanation"`
	AnswerFileIDs []string `json:"answerFileIds"`
	BlankCount    int      `json:"blankCount"`
}

type MechanicalPracticeResult struct {
	Correct     bool     `json:"correct"`
	Score       float64  `json:"score"`
	MaxScore    float64  `json:"maxScore"`
	Answers     []string `json:"answers"`
	Explanation string   `json:"explanation"`
}

func supportsProblemBank(direction string) bool {
	return direction == "机械" || direction == "电控" || direction == "视觉算法" || direction == "硬件"
}

func (a *App) validateMechanicalProblem(t *Task, u User, previousFiles []string) error {
	if t.Category != "problem" || !supportsProblemBank(t.Direction) {
		if t.MechanicalProblem != nil {
			return fail(400, "客观题配置仅适用于机械、电控、算法和硬件组题库")
		}
		return nil
	}
	p := t.MechanicalProblem
	if p == nil {
		if t.Direction == "机械" {
			return fail(400, "请选择机械题型并配置题目答案")
		}
		return nil // The other banks may use the existing programming judge.
	}
	if t.SubmissionType != "" || t.Kind == "code" || t.Judge != nil {
		return fail(400, "客观题直接在线作答，图纸题下载练习，不能同时配置文件提交或编程判题")
	}
	if p.Options == nil {
		p.Options = []string{}
	}
	if p.Answers == nil {
		p.Answers = []string{}
	}
	if p.AnswerFileIDs == nil {
		p.AnswerFileIDs = []string{}
	}
	p.Explanation = strings.TrimSpace(p.Explanation)
	if len(p.Explanation) > 20000 {
		return fail(400, "答案解析不能超过 20000 字节")
	}
	for i := range p.Answers {
		p.Answers[i] = strings.TrimSpace(p.Answers[i])
		if p.Answers[i] == "" || len(p.Answers[i]) > 1000 {
			return fail(400, "答案不能为空或超过 1000 字节")
		}
	}
	switch p.Type {
	case "single_choice", "multiple_choice":
		if len(p.Options) < 2 || len(p.Options) > 20 {
			return fail(400, "选择题须设置 2 至 20 个选项")
		}
		seen := map[string]bool{}
		for i := range p.Options {
			p.Options[i] = strings.TrimSpace(p.Options[i])
			if p.Options[i] == "" || len(p.Options[i]) > 2000 || seen[p.Options[i]] {
				return fail(400, "选项不能为空、重复或超过 2000 字节")
			}
			seen[p.Options[i]] = true
		}
		if err := validateMechanicalChoices(p, p.Answers); err != nil {
			return err
		}
		p.BlankCount = 0
	case "fill_blank":
		if len(p.Options) != 0 || len(p.Answers) < 1 || len(p.Answers) > 10 {
			return fail(400, "填空题须设置 1 至 10 个答案，不设置选择项")
		}
		p.BlankCount = len(p.Answers)
	case "drawing":
		if t.Direction != "机械" {
			return fail(400, "图纸自由练习仅适用于机械组题库")
		}
		if len(t.FileIDs) == 0 {
			return fail(400, "图纸题须上传至少一个题目图纸文件")
		}
		if len(p.Options) != 0 || len(p.Answers) != 0 {
			return fail(400, "图纸题仅供自由练习，可添加参考说明和答案文件，无需标准作答项")
		}
		p.BlankCount = 0
	default:
		return fail(400, "请选择单选、多选或填空；机械组还支持图纸练习")
	}
	if p.Type != "drawing" && len(p.AnswerFileIDs) != 0 {
		return fail(400, "只有图纸题可以附带可下载的参考答案文件")
	}
	if err := requireUpdatedFiles(a.store, u, p.AnswerFileIDs, previousFiles); err != nil {
		return err
	}
	return a.requireMechanicalLearningFiles(append(append([]string{}, t.FileIDs...), p.AnswerFileIDs...))
}

// Personal profile photos and recruitment resumes must never become shared
// question material or reference answers, even when selected by an administrator.
func (a *App) requireMechanicalLearningFiles(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	profiles, err := readAll[Profile](a.store, "SELECT payload FROM profiles")
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if (p.PhotoFileID != "" && includesID(ids, p.PhotoFileID)) || (p.AvatarFileID != "" && includesID(ids, p.AvatarFileID)) {
			return fail(400, "个人照片不能用作题目或参考答案文件，请上传教学资料")
		}
	}
	applications, err := readAll[TeamApplication](a.store, "SELECT payload FROM team_applications")
	if err != nil {
		return err
	}
	for _, application := range applications {
		if application.ResumeFileID != "" && includesID(ids, application.ResumeFileID) {
			return fail(400, "报名简历不能用作题目或参考答案文件，请上传教学资料")
		}
	}
	return nil
}

func validateMechanicalChoices(p *MechanicalProblem, answers []string) error {
	if len(answers) == 0 || len(answers) > len(p.Options) || p.Type == "single_choice" && len(answers) != 1 {
		return fail(400, "请选择有效数量的答案：单选题一项，多选题至少一项")
	}
	seen := map[string]bool{}
	for _, answer := range answers {
		index, err := strconv.Atoi(answer)
		if err != nil || index < 0 || index >= len(p.Options) || strconv.Itoa(index) != answer || seen[answer] {
			return fail(400, "选择项编号无效或重复")
		}
		seen[answer] = true
	}
	return nil
}

func (a *App) practiceMechanicalProblem(w http.ResponseWriter, r *http.Request, u User) error {
	t, err := a.store.task(r.PathValue("id"))
	if err != nil {
		return err
	}
	p := t.MechanicalProblem
	if t.Category != "problem" || !supportsProblemBank(t.Direction) || p == nil || (p.Type != "single_choice" && p.Type != "multiple_choice" && p.Type != "fill_blank") {
		return fail(400, "此接口仅用于选择或填空题；编程题请提交代码，图纸题可下载资料自由练习")
	}
	if err := a.requireTaskSubmission(t, u); err != nil {
		return err
	}
	var in struct {
		Answers []string `json:"answers"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	for i := range in.Answers {
		in.Answers[i] = strings.TrimSpace(in.Answers[i])
		if len(in.Answers[i]) > 1000 {
			return fail(400, "单个答案不能超过 1000 字节")
		}
	}
	if p.Type == "fill_blank" {
		if len(in.Answers) != len(p.Answers) {
			return fail(400, "请按题目顺序填写所有空的答案")
		}
	} else if err := validateMechanicalChoices(p, in.Answers); err != nil {
		return err
	}
	wanted, submitted := append([]string{}, p.Answers...), append([]string{}, in.Answers...)
	if p.Type == "multiple_choice" {
		sort.Strings(wanted)
		sort.Strings(submitted)
	}
	correct := len(wanted) == len(submitted)
	if correct {
		for i := range wanted {
			if wanted[i] != submitted[i] {
				correct = false
				break
			}
		}
	}
	result := MechanicalPracticeResult{Correct: correct, MaxScore: t.MaxScore, Answers: append([]string{}, p.Answers...), Explanation: p.Explanation}
	status, feedback := "changes_requested", "答案不正确，请查看参考答案后继续练习。"
	if correct {
		result.Score = t.MaxScore
		status, feedback = "approved", "回答正确。"
	}
	if p.Explanation != "" {
		feedback += "\n" + p.Explanation
	}
	var version int
	if err := a.store.row("SELECT COALESCE(MAX(version),0) FROM submissions WHERE task_id=? AND user_id=?", t.ID, u.ID).Scan(&version); err != nil {
		return err
	}
	submission := Submission{ID: randomID(), TaskID: t.ID, UserID: u.ID, UserName: u.Name, FileIDs: []string{}, Content: encoded(in.Answers), Version: version + 1, Status: status, Score: &result.Score, Feedback: feedback, CreatedAt: now(), ReviewedAt: now(), ReviewedBy: "system"}
	if err := a.store.exec("INSERT INTO submissions(id,task_id,user_id,version,payload) VALUES(?,?,?,?,?)", submission.ID, submission.TaskID, submission.UserID, submission.Version, encoded(submission)); err != nil {
		return err
	}
	a.activity(u, "完成题库练习", t.Title)
	respond(w, 201, map[string]any{"item": submission, "result": result})
	return nil
}
