# 协作与发布

## 工作方式

项目采用任务分支、PR 和 Squash 合并。本地完成验证，在 PR 中记录结果，通过评审后合并。GitHub Actions 配置提供自动检查与标签发布能力。

## 分支与 PR

每个 PR 聚焦一个可独立评审的结果。开始新任务时，从同步后的 `main` 创建分支；已在进行中的任务继续使用当前分支。

```bash
git switch main
git pull --ff-only
git switch -c feat/workspace-api
```

提交和合并流程：

1. 完成实现及对应文档，检查差异。代码变更运行 `make check`；纯文档变更检查链接和内容，PR 中说明验证方式。
2. 在本地校验实际 PR 标题，例如：`node .github/scripts/check-pr-title.cjs 'feat(workspace): add workspace creation'`。
3. 提交、推送任务分支并创建目标为 `main` 的 PR，说明变更结果、验证命令与结果。
4. 评审最终差异。更新代码或解决冲突后，重新验证受影响的内容。
5. 按仓库规则完成检查与评审后，由维护者选择 Squash and merge，保留符合 Conventional Commits 的标题和必要的破坏性变更说明。

PR 模板中的检查项按实际情况填写，未运行的项目写明原因。

## 提交约定

提交信息和 PR 标题使用 [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)：

```text
docs: establish project documentation
feat(workspace): add workspace creation
fix(api): handle invalid workspace IDs
feat(api)!: change workspace response format
```

本仓库标题检查接受 `feat`、`fix`、`docs`、`style`、`refactor`、`perf`、`test`、`build`、`ci`、`chore`、`revert`。scope 可省略；使用时由小写字母、数字、点、下划线、斜杠、连字符组成，并以字母或数字开头。描述不能为空，标题不能有首尾空白或换行。

破坏性变更使用 `!` 或提交正文中的 `BREAKING CHANGE:`，并解释迁移影响。Squash 后进入 `main` 的提交将成为未来版本分析的依据；当前标题脚本只检查格式，不决定版本号。

## Actions 与仓库规则

仓库包含以下工作流：

| Workflow | 触发条件 | 检查 / 结果 |
| --- | --- | --- |
| `CI` | 发往 `main` 的 PR、`main` 推送、手动触发 | `Go CI` 执行 `make check` |
| `PR title` | PR 创建、改标题、更新代码、重新打开、转为待评审 | `PR Title` 校验标题格式 |
| `Release` | 推送 `v*` 标签 | 验证、打包并创建 GitHub Release |

主分支通过 PR 合并，并禁止强推和删除。仓库规则与 workflow 分别配置；如需强制自动验证，在确认 `Go CI` 和 `PR Title` 能成功运行后，将它们设为必需检查。workflow 文件不会自动配置这些规则。

CI 与标题检查声明只读仓库权限；发布任务使用 `contents: write`。Actions 固定 commit SHA，并在注释中标注版本；Go 版本从 `go.mod` 读取。

## 发布现状

当前由维护者选择 `vMAJOR.MINOR.PATCH` 版本，脚本不接受预发布后缀。尚未接入 semantic-release，也没有自动部署。

发布前，在已合并并同步的 `main` 上本地验证、打包：

```bash
make check
make dist VERSION=v0.1.0
```

示例版本按实际发布替换。产物位于 `dist/v0.1.0/`，包含 Linux amd64 / arm64 的 tar.gz 包和 `checksums.txt`。本地打包不会上传产物。

在已合并且检查通过的 `main` 上创建并推送版本标签，触发现有发布工作流：

```bash
git switch main
git pull --ff-only
git tag -a v0.1.0 -m 'Release v0.1.0'
git push origin v0.1.0
```

工作流要求标签提交已进入远程 `main`，重新执行 `make check`，打包并生成 Release notes、上传附件。它使用 `GITHUB_TOKEN`，无需额外 PAT。每次正式发布使用新标签，保留已发布标签指向的提交。

下载发布包后，在同一目录使用 `sha256sum --check checksums.txt --ignore-missing` 验证文件，再解压运行 `mini-coder`。

## 后续自动化

[semantic-release](https://semantic-release.org/intro/) 可根据提交历史确定版本、生成版本说明并发布。后续单独设计其版本规则、执行环境与 Go 产物发布方式，配置与 Conventional Commits 一致的解析规则，并用 dry-run 验证；接入时统一版本和标签的生成责任，替换当前手工选版本的流程。
