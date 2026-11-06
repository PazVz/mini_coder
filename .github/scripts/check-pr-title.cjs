const title = process.argv[2] ?? process.env.PR_TITLE ?? "";
const pattern = /^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9][a-z0-9._/-]*\))?!?: \S[^\r\n]*$/u;

if (title !== title.trim() || !pattern.test(title)) {
  console.error("PR title must use: type(scope): description");
  console.error("Example: feat(workspace): add workspace creation");
  console.error("Scope is optional; use ! before : for a breaking change.");
  console.error("Allowed types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.");
  process.exitCode = 1;
} else {
  console.log("PR title is valid.");
}
