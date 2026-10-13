package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func createUser(s *Store, name, email, password, role, team, direction string) (User, error) {
	u := User{ID: randomID(), Name: name, Email: email, Role: role, Team: team, Direction: direction, JoinedAt: now()}
	if roleID, ok := map[string]string{"admin@demo.local": "demo-admin", "mentor@demo.local": "demo-mentor", "interviewer@demo.local": "demo-interviewer", "student@demo.local": "demo-student"}[email]; ok {
		u.ID = roleID
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return u, err
	}
	err = s.exec("INSERT INTO users(id,email,password_hash,payload) VALUES(?,?,?,?)", u.ID, u.Email, string(hash), encoded(u))
	return u, err
}
func initialize(s *Store, demo bool) error {
	if err := s.exec("CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY,value TEXT NOT NULL)"); err != nil {
		return err
	}
	var marker string
	err := s.row("SELECT value FROM metadata WHERE key='demo_seeded'").Scan(&marker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !demo && marker == "true" {
		return fmt.Errorf("this database contains public demo credentials; use a fresh production DATA_DIR or DATABASE_URL")
	}
	if _, err = s.settings(); errors.Is(err, sql.ErrNoRows) {
		deadline := ""
		if demo {
			deadline = time.Now().UTC().AddDate(0, 0, 30).Format(time.RFC3339)
		}
		if err = s.saveSettings(Settings{BrandName: "QUT机器人战队", LogoURL: "/mark.png", TeamName: "沧溟", SecondaryTeamName: "MOVE", Competitions: []string{"RoboCon", "RoboMaster"}, Organization: "青岛理工大学机器人战队", Season: strconv.Itoa(time.Now().Year()+1) + " 赛季", ApplicationOpen: demo, ApplicationDeadline: deadline, Announcement: "欢迎来到战队工作台。在这里，一起把想法变成会动的机器人。"}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err = migrateBranding(s); err != nil {
		return err
	}
	if err = migrateDefaultLogo(s); err != nil {
		return err
	}
	if demo {
		if marker == "true" {
			return nil
		}
		var users int
		if err = s.row("SELECT COUNT(*) FROM users").Scan(&users); err != nil {
			return err
		}
		if users != 0 {
			return fmt.Errorf("refusing to seed demo data into a nonempty database")
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		transactional := &Store{db: s.db, postgres: s.postgres, tx: tx}
		if err = seedDemo(transactional); err != nil {
			return err
		}
		if err = transactional.exec("INSERT INTO metadata(key,value) VALUES('demo_seeded','true')"); err != nil {
			return err
		}
		return tx.Commit()
	}
	users, err := readAll[User](s, "SELECT payload FROM users")
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Role == "admin" {
			return nil
		}
	}
	email, valid := normalizedEmail(os.Getenv("ADMIN_EMAIL"))
	password := os.Getenv("ADMIN_PASSWORD")
	name := env("ADMIN_NAME", "管理员")
	if !valid || len(password) < 12 || len(password) > 72 || len(name) > 100 {
		return fmt.Errorf("first production startup requires ADMIN_EMAIL and ADMIN_PASSWORD (12–72 bytes); ADMIN_NAME is optional")
	}
	_, err = createUser(s, name, email, password, "admin", "", "")
	return err
}
func seedDemo(s *Store) error {
	admin, err := createUser(s, "陈知行", "admin@demo.local", "DemoAdmin!2026", "admin", "RoboMaster", "电控")
	if err != nil {
		return err
	}
	mentor, err := createUser(s, "林亦舟", "mentor@demo.local", "DemoMentor!2026", "mentor", "RoboMaster", "电控")
	if err != nil {
		return err
	}
	interviewer, err := createUser(s, "周予安", "interviewer@demo.local", "DemoInterview!2026", "interviewer", "RoboCon", "机械")
	if err != nil {
		return err
	}
	student, err := createUser(s, "许星禾", "student@demo.local", "DemoStudent!2026", "student", "RoboMaster", "电控")
	if err != nil {
		return err
	}
	allUsers := []User{student}
	names := []string{"陆向晨", "沈沐遥", "顾清越", "唐予川", "江晚晴", "苏亦宁", "程砚秋", "叶知夏", "温书遥"}
	dirs := []string{"机械", "视觉算法", "电控", "硬件", "运营", "机械", "视觉算法", "电控", "机械"}
	states := []string{"interview", "interview", "screening", "submitted", "submitted", "accepted", "screening", "accepted", "rejected"}
	for i, name := range names {
		team := "RoboMaster"
		if i%3 == 0 {
			team = "RoboCon"
		}
		dir := ""
		userTeam := ""
		if states[i] == "accepted" {
			dir = dirs[i]
			userTeam = team
		}
		u, e := createUser(s, name, fmt.Sprintf("member%02d@demo.local", i+1), randomID(), "student", userTeam, dir)
		if e != nil {
			return e
		}
		allUsers = append(allUsers, u)
	}
	for i, u := range allUsers {
		team, dir, state := "RoboMaster", "电控", "accepted"
		if i > 0 {
			dir = dirs[i-1]
			state = states[i-1]
			if (i-1)%3 == 0 {
				team = "RoboCon"
			}
		}
		created := time.Now().UTC().AddDate(0, 0, -12+i).Format(time.RFC3339)
		ap := Applicant{ID: randomID(), UserID: u.ID, Name: u.Name, Email: u.Email, Phone: "1380000" + fmt.Sprintf("%04d", i), University: "示例科技大学（虚构）", Major: map[string]string{"机械": "机械设计制造及其自动化", "电控": "自动化", "视觉算法": "计算机科学与技术", "硬件": "电子信息工程", "运营": "传播学"}[dir], Year: "大一", Team: team, Direction: dir, Motivation: "希望在真实的机器人项目中学习，将课堂知识用于解决工程问题，和队友一起完成赛季目标。", Experience: "完成过课程设计和基础实验，愿意持续记录学习过程并参与团队协作。", Status: state, CreatedAt: created, UpdatedAt: created}
		if state == "interview" {
			ap.InterviewerID = interviewer.ID
			ap.InterviewAt = time.Now().UTC().Add(time.Duration(24+i*2) * time.Hour).Format(time.RFC3339)
		}
		if state == "accepted" {
			score := float64(86 + i)
			ap.Score = &score
			ap.Notes = "学习目标清晰，基础扎实。建议通过阶段任务继续观察工程实践能力。"
		}
		if err = s.exec("INSERT INTO applicants(id,user_id,payload) VALUES(?,?,?)", ap.ID, ap.UserID, encoded(ap)); err != nil {
			return err
		}
	}
	definitions := []struct {
		title, desc, dir, team, kind, difficulty string
		days                                     int
		required                                 bool
	}{
		{"从零驱动第一颗电机", "理解 PWM、编码器反馈与闭环控制。完成电机速度闭环，提交代码仓库、阶跃响应曲线和 2 分钟演示视频。\n\n验收标准：能解释 PID 参数作用；达到目标转速并稳定保持；具备断连保护。", "电控", "RoboMaster", "hardware", "入门", 5, true},
		{"C / C++ 基础与串口协议解析", "完成环形缓冲区与定长帧解析，实现校验、异常帧丢弃和测试用例。提交仓库链接与思路说明。可在系统设置关联的 Hydro 中完成配套编程练习。", "电控", "", "code", "入门", 9, true},
		{"抓取机构方案设计", "根据任务约束设计一套稳定抓取机构。提交 CAD 链接、关键零件工程图、驱动选型依据与风险分析。", "机械", "RoboCon", "document", "进阶", 12, true},
		{"相机标定与坐标变换", "拍摄标定板并完成相机内参标定，分析重投影误差，说明相机、机器人与场地坐标系之间的关系。", "视觉算法", "", "code", "进阶", 7, true},
		{"认识你的赛季：规则阅读笔记", "阅读本队当前赛季的正式规则，记录规则版本、原文出处、关键约束和三个不确定的问题。此任务由导师人工审核，不自动判题。", "", "", "quiz", "入门", 3, true},
		{"电源保护与线束检查清单", "梳理供电链路、保险与保护方案，制作可复用的上电检查清单。提交原理说明与实际检查记录。", "硬件", "", "hardware", "进阶", 14, false},
	}
	tasks := []Task{}
	for i, d := range definitions {
		t := Task{ID: randomID(), Title: d.title, Description: d.desc, Direction: d.dir, Team: d.team, Kind: d.kind, Difficulty: d.difficulty, DueAt: time.Now().UTC().AddDate(0, 0, d.days).Format(time.RFC3339), MaxScore: 100, Required: d.required, CreatedBy: mentor.ID, CreatedAt: time.Now().UTC().AddDate(0, 0, -6+i).Format(time.RFC3339)}
		if err = s.exec("INSERT INTO tasks(id,payload) VALUES(?,?)", t.ID, encoded(t)); err != nil {
			return err
		}
		tasks = append(tasks, t)
	}
	for i, taskIndex := range []int{0, 1, 4} {
		status := "pending"
		var score *float64
		feedback, reviewedBy, reviewedAt := "", "", ""
		if i == 0 {
			status = "changes_requested"
			v := 72.
			score = &v
			feedback = "功能已基本完成。请补充不同负载下的速度曲线，并说明断连保护的触发条件。"
			reviewedBy = mentor.ID
			reviewedAt = time.Now().UTC().Add(-20 * time.Hour).Format(time.RFC3339)
		}
		v := Submission{ID: randomID(), TaskID: tasks[taskIndex].ID, UserID: student.ID, UserName: student.Name, Content: "已完成第一阶段实现，提交测试记录与设计说明。\n\n目前发现的问题：负载变化时有短暂超调，下一步会补充参数对比实验。", RepoURL: "https://github.com/example/robot-learning", Version: 1, Status: status, Score: score, Feedback: feedback, ReviewedBy: reviewedBy, ReviewedAt: reviewedAt, CreatedAt: time.Now().UTC().Add(time.Duration(-30+i) * time.Hour).Format(time.RFC3339)}
		if err = s.exec("INSERT INTO submissions(id,task_id,user_id,version,payload) VALUES(?,?,?,?,?)", v.ID, v.TaskID, v.UserID, v.Version, encoded(v)); err != nil {
			return err
		}
	}
	activities := []struct {
		u              User
		action, target string
	}{{student, "提交任务成果", "C / C++ 基础与串口协议解析"}, {mentor, "审核任务成果", "从零驱动第一颗电机 · 许星禾"}, {admin, "完成录取确认", "新成员已加入电控方向"}, {interviewer, "安排面试", "RoboCon · 机械方向"}, {mentor, "发布培养任务", "认识你的赛季：规则阅读笔记"}}
	for i, x := range activities {
		v := Activity{ID: randomID(), Actor: x.u.Name, Action: x.action, Target: x.target, CreatedAt: time.Now().UTC().Add(time.Duration(-i-1) * time.Hour).Format(time.RFC3339)}
		if err = s.exec("INSERT INTO activities(id,user_id,created_at,payload) VALUES(?,?,?,?)", v.ID, x.u.ID, v.CreatedAt, encoded(v)); err != nil {
			return err
		}
	}
	return nil
}
