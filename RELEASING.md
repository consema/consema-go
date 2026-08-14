# Consema Go 发布流程（tag + GitHub release；Go proxy 待域名就绪）

本文件是 consema-go 仓库的发布操作手册（六仓统一纪律见 consema 仓库根
`RELEASING.md`）。**Go 没有构建/发布步骤——推送 tag `vX.Y.Z` 即发布**：
发布载体是 GitHub Release（tag + 自动生成 notes）；Go proxy
（proxy.golang.org）收录模块版本的前提是 `consema.dev` 域名可解析（DNS +
vanity import meta）且模块路径/仓库布局就绪——域名就绪前
`go get consema.dev/consema` 不被 proxy 服务（G108，对抗审计 2026-08-13：
旧文把 proxy 自动拾取作为现行机制陈述；模块 `consema.dev/consema`，模块
文档取 `go/README.md`）。

## 1. 发布步骤（人执行的部分）

1. **版本 bump**：Go 无 manifest 版本（go.mod 不声明包版本），版本号由
   发布火车承载——改仓根 `README.md` 的 `Version:` 行
   （`check-version-consistency` 门禁要求该行存在；随六仓同步 bump，
   当前 1.0.0-rc.1）。
2. **CHANGELOG 策展**：记录本版本变更；跨语言变更同步到
   consema 仓库根 `CHANGELOG.md`（G156，对抗审计 2026-08-13：旧文指向
   `docs/CHANGELOG.md`，那是 870 字节勘误页而非历史记录——真实历史在根
   `CHANGELOG.md`）。
3. **质量门禁全绿**：main 分支 CI `check (all gates green)` 全绿
   （清单见各仓 ci 配置）。
4. **打 tag 并推送**（发布动作的唯一触发点）：
   ```bash
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```
   推送后 `.github/workflows/release.yml` 实际执行顺序（G167，对抗审计
   2026-08-13：旧文把顺序写成"先校验后 provision"，实际 workflow 先做
   conformance 数据 provision、再跑校验）：
   - `verify` job（ubuntu-latest）：provision conformance 数据（多仓
     checkout，规范仓钉 096e5f8）→ 校验 tag 指向 origin/main HEAD →
     校验 tag↔版本一致（tag 去掉 `v` 前缀必须等于仓根 `README.md` 的
     `Version:` 行，不一致即 exit 1 中止）→ 在 tag 上重跑完整门禁
     （gofmt + vet + build + test + race，含 conformance 数据，与 ci-go.yml
     同款）；
   - `differential` job（windows-latest，G106）：在 tag 上重跑四个跨语言
     差分 harness（byte parity / normalized / protocol exchange / shared
     conformance -StrictSkips，与 ci-go.yml go-differential 同款）；
   - `github-release` job：`verify` 与 `differential` 都通过后用
     `softprops/action-gh-release@v2` 创建 GitHub Release（自动生成
     notes），**需要 `contents: write` 权限**。

## 2. Go proxy 收录（待域名就绪；无需凭证）

- `consema.dev` 域名解析就绪后，tag 推送会使 proxy.golang.org 在首次
  `go get` 时收录版本（G108：域名就绪前本节不生效）：
  ```bash
  go get consema.dev/consema@vX.Y.Z
  ```
  （决策点：`go.mod` 位于 `go/` 子目录，Go 按模块路径解析仓库子目录，
  不会自动补子目录后缀——域名就绪时 vanity import meta 必须按模块路径
  提供服务，故模块路径须为 `consema.dev/consema/go`，或将 `go.mod`
  移至仓根后使用 `consema.dev/consema`；G026，对抗审计 2026-08-14：
  旧文「无需路径后缀」与 Go 模块解析规则矛盾。版本校验以 `vX.Y.Z`
  tag 为准。）
- 用户侧核对：pkg.go.dev/consema.dev/consema 的版本列表出现新版本
  （首次收录可能需要几分钟到一小时）。
- 注意事项：tag 一经 Go proxy 收录即**不可变**——不要删除/重推已收录
  的 tag，否则下游构建会出现校验和错误。发布前务必确认版本号正确。

## 3. goreleaser 二进制发布（P2 可选，暂不接线）

Go CLI（`go/cmd/consema`，路线图 §16.6）的跨平台二进制发布后续用
goreleaser 接线（tag 触发，产出 GitHub Release assets：各平台可执行
文件 + checksum + SBOM）；届时在 release.yml 增加 goreleaser job
（`goreleaser/goreleaser-action@v6`，`permissions: contents: write`），
**且必须以 `-tags release` 构建**——`CONSEMA_APPLY_INTERRUPT_AFTER` /
`CONSEMA_APPLY_WRITE_FAILURE` 注入缝在 release 构建中被编译掉，发布
二进制绝不读取这两个变量（G114，对抗审计 2026-08-14，对齐 rs G045）。
本 workflow 当前保持最小化（仅测试确认 + GitHub release）。
