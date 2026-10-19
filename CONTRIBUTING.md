# 参与 MR System

欢迎提交 Issue、改进界面、修复问题和完善部署文档。题目内容请优先提交到独立仓库 [MR_question_bank](https://github.com/goldenfishs/MR_question_bank)，本仓库维护网站与同步逻辑。

## 本地开发

需要 Node.js 22 与 Go 1.26。运行 `npm --prefix frontend ci`、`bash scripts/dev.sh`，使用独立本地演示数据库。演示账号见 README，不要将演示配置用于公开部署。

提交前按修改范围运行：

```sh
npm --prefix frontend run build
cd backend
go vet ./...
go test ./...
```

提交 PR 时说明用户遇到的问题、修改后的行为及已完成的验证。涉及数据库、权限、题目同步、邮件或判题时，请补充能覆盖实际风险的测试。

## 数据与权限

不要提交 `.env`、用户资料、提交记录、上传文件、生产数据库、API Token 或内部培训题。题库同步仅获取数据，不执行题库仓库脚本。学生接口不能返回标准答案或隐藏用例；公开题库的原始测试数据仍可由其开源仓库读到，不是正式考试的保密来源。

已有提交或作业引用的题目需要保留历史内容。新增权限必须在服务端校验，前端隐藏按钮不能代替权限检查。

## 发布

带版本的安装包与 Docker 镜像由维护者通过 `vMAJOR.MINOR.PATCH` 标签构建。发布流程会执行测试、构建 amd64/arm64 镜像并生成校验文件。不要在 PR 工作流中引入生产凭据。

项目使用 MIT 许可，提交贡献需确保有权使用并保留必要署名。
