# Contributing to consema-go（Consema Go 实现）

Consema 六仓拆分的 Go 仓：本仓承载 Go 实现（`go/` 模块 `consema.dev/consema`）
与跨语言差分验证工具；规范权威（RFC / docs / 路线图 / conformance suites）
在[规范仓](https://github.com/consema/consema)。

**社区治理以规范仓主文档为准**：报 bug / 提 feature / RFC 流程 / 提交规范 /
评审规范 / 标签体系 / 发布纪律 / 行为准则，一律参见
[consema/CONTRIBUTING.md](https://github.com/consema/consema/blob/main/CONTRIBUTING.md)。
本文件只列本仓特有内容。

## 开发环境

- Go 1.26（`go.mod` 声明的 toolchain；CI 使用对应版本）。
- 无运行时第三方依赖。

## 构建与测试

```text
cd go
go build ./...
go test ./...
go test -race ./...
```

## 贡献点

- **Go 实现**：`go/` 模块（core / graph / protocol / document + 八格式家族
  + CLI）；完整文档见 [go/README.md](go/README.md)。
- **差分 harness**：`scripts/` 跨语言差分验证（byte parity / normalized
  differential / protocol exchange / shared conformance）：
  `go-verify-byte-parity.ps1`、`go-verify-normalized-differential.ps1`、
  `go-verify-protocol-exchange.ps1`、`go-verify-shared-conformance.ps1`。
  脚本构建 consema-rs 的 Rust emitter 对拍本实现。
- **Conformance 数据同步**：conformance 数据来自规范仓 checkout（CI 多仓
  模式），权威在规范仓，改动必须回规范仓提交后再同步。

## CI 门禁

`.github/workflows/ci-go.yml`：gofmt / vet / build / test / race 门禁 +
Go-Rust 差分门禁（windows-latest 多仓 checkout）。push 到 main 或 PR 均
触发；PR 另受 pr-labels.yml 的 kind 标签门禁约束（标签见规范仓
.github/LABELS.md）。

## 发布与安全

- 发布：本仓 [RELEASING.md](RELEASING.md)（Go proxy `consema.dev/consema`
  + GitHub Release，tag 即发布；tag 不可变，发布前确认）。
- 安全：[SECURITY.md](SECURITY.md)；披露统一走规范仓 SECURITY.md 的渠道。
