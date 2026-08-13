# Changelog

Consema 遵循 Semantic Versioning。本仓变更记录以规范仓 CHANGELOG 为权威；完整历史与跨语言时间线见 github.com/consema/consema 的 CHANGELOG.md。

## 1.0.0-rc.1（2026-08-12）

六仓拆分落地：本仓自规范仓（github.com/consema/consema）拆分独立（2026-08-12），承载 Go 实现（go 1.26，module `consema.dev/consema`，运行时零第三方依赖）。

- 0.14.0-0.19.0 里程碑摘要（G0.1-G5.6 全部交付，排期见规范仓 `https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md`）：0.14.0 core 15-kind + PVCE/PGCE + protocol v1-v7（41 条 contract / 187 码，2026-08-07 decision record 启动）→ 0.15.0 document + JSON family + TOML → 0.16.0 YAML/INI/Properties → 0.17.0 XML/plist → 0.18.0 HCL 与全操作 parity → 0.19.0 CLI（11 命令 + plan→apply）与双语言一致性；
- 验证基线：20 包构建/静态检查通过（17 生产包 + cmd/consema、cmd/consema-conformance 两个 CLI + sdk_chain 示例；pilot/ 为纯测试包，是第 21 个包，不计入生产面——G068 对抗审计 2026-08-13），19 个含测试包 go test 全绿（LF 规范态含 race）、conformance 519/519（18 套 / 聚合 digest cfd6e296 共钉；2026-08-12 P2-B 向量补强 508→519）、差分 108/108 双向、协议交换 83/83、字节 parity 68/68、capability parity PASS、16 fuzz targets 30s clean-run；
- CI（ci-go.yml）：6 个 job（go-matrix 1.26.x / 1.26.5
  gofmt/vet/build/test/race、coverage、go-differential、check-version-consistency、
  examples、check 聚合门禁；G123）；
- 完整历史与跨语言时间线见规范仓 CHANGELOG。
