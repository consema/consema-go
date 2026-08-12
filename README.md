# Consema Go SDK（consema-go）

Consema 语言中立契约（RFC 0016）的 **Go 实现**仓库。本仓库是 Consema 六仓
拆分中的 Go 仓：规范权威（RFC、docs、路线图、跨语言 conformance suites）在
[github.com/consema/consema](https://github.com/consema/consema)；本仓承载
Go 实现与跨语言差分验证工具。

## 布局

- `go/`：Go 模块（`go.mod`，module `consema.dev/consema`，go 1.26）。完整文档
  见 [go/README.md](go/README.md)（全部里程碑 0.14.0-0.19.0 G0.1-G5.6 已交付：
  core / graph / protocol / document + 八格式家族 + CLI）。
- `scripts/`：跨语言差分验证脚本（byte parity / normalized differential /
  protocol exchange / shared conformance）。脚本构建 consema-rs 的 Rust
  emitter 并对拍 Go 实现；Rust 侧来自 consema-rs 仓 checkout（CI 多仓模式），
  conformance 数据来自规范仓 checkout。
- `.github/workflows/ci-go.yml`：Go 1.26 门禁（gofmt/vet/build/test/race）
  与 Go-Rust 差分门禁（windows-latest 多仓 checkout）。

## 构建与测试

```text
cd go
go build ./...
go test ./...
go test -race ./...
```

## 链接

- 规范仓（RFC / docs / 路线图）：https://github.com/consema/consema
- Rust 参考实现：https://github.com/consema/consema-rs
