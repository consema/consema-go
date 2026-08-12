# Consema Go 发布流程（Go proxy + GitHub release）

本文件是 consema-go 仓库的发布操作手册（六仓统一纪律见 consema 仓库根
`RELEASING.md`）。**Go 没有构建/发布步骤——推送 tag `vX.Y.Z` 即发布**：
Go proxy（proxy.golang.org）自动从 tag 收录模块版本（模块
`consema.dev/consema`，模块文档取 `go/README.md`）。

## 1. 发布步骤（人执行的部分）

1. **版本 bump**：Go 无 manifest 版本（go.mod 不声明包版本），版本号由
   发布火车承载——改仓根 `README.md` 的 `Version:` 行
   （`check-version-consistency` 门禁要求该行存在；随六仓同步 bump，
   当前 1.0.0-rc.1）。
2. **CHANGELOG 策展**：记录本版本变更；跨语言变更同步到
   consema 仓库 `docs/CHANGELOG.md`。
3. **质量门禁全绿**：main 分支 CI `check (all gates green)` 通过
   （go-matrix / go-differential / coverage / check-version-consistency）。
4. **打 tag 并推送**（发布动作的唯一触发点）：
   ```bash
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```
   推送后 `.github/workflows/release.yml` 做两件事：
   - `verify` job：在 tag 上重跑完整门禁
     （gofmt + vet + build + test + race，含 conformance 数据 provision，
     与 ci-go.yml 同款）；
   - `github-release` job：用 `softprops/action-gh-release@v2` 创建
     GitHub Release（自动生成 notes），**需要 `contents: write` 权限**。

## 2. Go proxy 收录（无需凭证）

- tag 推送后，proxy.golang.org 会在首次 `go get` 时收录版本：
  ```bash
  go get consema.dev/consema@vX.Y.Z
  ```
  （模块根为 `go/` 子目录，但 `go get` 按模块路径解析，无需路径后缀；
  版本校验以 `vX.Y.Z` tag 为准。）
- 用户侧核对：pkg.go.dev/consema.dev/consema 的版本列表出现新版本
  （首次收录可能需要几分钟到一小时）。
- 注意事项：tag 一经 Go proxy 收录即**不可变**——不要删除/重推已收录
  的 tag，否则下游构建会出现校验和错误。发布前务必确认版本号正确。

## 3. goreleaser 二进制发布（P2 可选，暂不接线）

Go CLI（`go/cmd/consema`，路线图 §16.6）的跨平台二进制发布后续用
goreleaser 接线（tag 触发，产出 GitHub Release assets：各平台可执行
文件 + checksum + SBOM）；届时在 release.yml 增加 goreleaser job
（`goreleaser/goreleaser-action@v6`，`permissions: contents: write`），
本 workflow 当前保持最小化（仅测试确认 + GitHub release）。
