package main

import "strings"

// Questions remain individually addressable for judging, drafts and grading.
// Group metadata provides a shared prompt without introducing parent scores.
func validateQuestionGroups(questions []TaskQuestion) error {
	type group struct{ title, description string }
	seen := map[string]group{}
	previous := ""
	for i := range questions {
		q := &questions[i]
		if err := validateIllustration(q.IllustrationURL, q.IllustrationAlt); err != nil {
			return err
		}
		q.GroupID = strings.TrimSpace(q.GroupID)
		q.GroupTitle = strings.TrimSpace(q.GroupTitle)
		q.GroupDescription = strings.TrimSpace(q.GroupDescription)
		if q.SourceProblemID != "" && !validTaskQuestionID(q.SourceProblemID) {
			return fail(400, "来源题目编号无效")
		}
		if q.GroupID == "" {
			if q.GroupTitle != "" || q.GroupDescription != "" {
				return fail(400, "请为大题设置编号")
			}
			previous = ""
			continue
		}
		if !validTaskQuestionID(q.GroupID) || q.GroupTitle == "" || len(q.GroupTitle) > 200 || len(q.GroupDescription) > 20000 {
			return fail(400, "请填写有效的大题标题和说明")
		}
		current := group{q.GroupTitle, q.GroupDescription}
		if old, ok := seen[q.GroupID]; ok {
			if previous != q.GroupID || old != current {
				return fail(400, "同一大题的小题必须连续排列，且共用相同的大题标题和说明")
			}
		} else {
			seen[q.GroupID] = current
		}
		previous = q.GroupID
	}
	return nil
}

func validateIllustration(url, alt string) error {
	if url != "" {
		name := strings.TrimPrefix(url, "/question-bank/")
		if name == url || !strings.HasSuffix(name, ".svg") || len(name) > 100 {
			return fail(400, "题目示意图须使用内置题库图片")
		}
		for _, ch := range strings.TrimSuffix(name, ".svg") {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return fail(400, "题目示意图路径不合法")
			}
		}
		if name == ".svg" || strings.TrimSpace(alt) == "" || len(alt) > 1000 {
			return fail(400, "请填写有效的题目示意图说明")
		}
	} else if alt != "" {
		return fail(400, "题目示意图说明需要对应的图片")
	}
	return nil
}
