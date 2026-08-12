# Changelog

Consema 遵循 Semantic Versioning。本仓变更记录以规范仓 CHANGELOG 为权威；完整历史与跨语言时间线见 github.com/consema/consema 的 CHANGELOG.md。

## 1.0.0-rc.1（2026-08-12）

六仓拆分落地：本仓自规范仓（github.com/consema/consema）拆分独立（2026-08-12），承载 Go 实现（go 1.24，module `consema.dev/consema`，运行时零第三方依赖）。

- 0.14.0-0.19.0 里程碑摘要（G0.1-G5.6 全部交付，排期见规范仓 `docs/go-implementation-plan.md`）：0.14.0 core 15-kind + PVCE/PGCE + protocol v1-v7（41 条 contract / 187 码，2026-08-07 decision record 启动）→ 0.15.0 document + JSON family + TOML → 0.16.0 YAML/INI/Properties → 0.17.0 XML/plist → 0.18.0 HCL 与全操作 parity → 0.19.0 CLI（11 命令 + plan→apply）与双语言一致性；
- 验证基线：19 包全绿（LF 规范态含 race）、conformance 519/519（18 套 / 聚合 digest cfd6e296 共钉；2026-08-12 P2-B 向量补强 508→519）、差分 108/108 双向、协议交换 83/83、字节 parity 68/68、capability parity PASS、16 fuzz targets 30s clean-run；
- CI（ci-go.yml）：Go 门禁（go-matrix 1.24.x / 1.25.x / 1.26.5，
  gofmt/vet/build/test/race）+ Go-Rust 差分门禁；
- 完整历史与跨语言时间线见规范仓 CHANGELOG。
