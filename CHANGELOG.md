# Changelog

Consema 遵循 Semantic Versioning。本仓变更记录以规范仓 CHANGELOG 为权威；完整历史与跨语言时间线见 github.com/consema/consema 的 CHANGELOG.md。

## 1.0.0-rc.1（2026-08-10；版本推进 commit 日期——规范仓 2209582 "Land 1.0.0-rc.1"，六仓统一，G063 对抗审计 2026-08-14）

六仓拆分落地：本仓自规范仓（github.com/consema/consema）拆分独立（2026-08-12），承载 Go 实现（go 1.26，module `consema.dev/consema`，运行时零第三方依赖）。

- 0.14.0-0.19.0 里程碑摘要（G0.1-G5.6 全部交付；G5.4 三平台验证中 macOS 腿 pending——见 go/README.md「Three-platform verification」，G028 对抗审计 2026-08-14。排期见规范仓 `https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md`）：0.14.0 core 15-kind + PVCE/PGCE + protocol v1-v7（41 条 contract / 187 码，2026-08-07 decision record 启动）→ 0.15.0 document + JSON family + TOML → 0.16.0 YAML/INI/Properties → 0.17.0 XML/plist → 0.18.0 HCL 与全操作 parity → 0.19.0 CLI（11 命令 + plan→apply）与双语言一致性；
- 验证基线：22 包构建/静态检查通过（20 个含测试包 go test 全绿：17 生产包 + cmd/consema、cmd/consema-conformance 两个 CLI + pilot 测试包；examples/sdk_chain 与 examples/quickstart 两个示例无测试文件，由 CI examples job 编译运行——G064 对抗审计 2026-08-14：旧文「20 包/19 含测试」漏计差分测试包与 quickstart，实测 22 包 / 20 含测试）、conformance 519/519（18 套 / 聚合 digest cfd6e296 共钉；2026-08-12 P2-B 向量补强 508→519）、差分 108/108 双向、协议交换 83/83、字节 parity 68/68、capability parity PASS、16 fuzz targets 30s clean-run；
- CI（ci-go.yml）：7 个 job（go-matrix 1.26.0 / 1.26.5
  gofmt/vet/build/test/race、coverage、go-differential、govulncheck、
  check-version-consistency、examples、check 聚合门禁；G064）；
- 完整历史与跨语言时间线见规范仓 CHANGELOG。
