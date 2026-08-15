# Consema Go SDK（consema-go）

![CI](https://img.shields.io/github/actions/workflow/status/consema/consema-go/ci-go.yml?branch=main)
![License](https://img.shields.io/github/license/consema/consema-go)

<!-- G063 (adversarial audit, 2026-08-14): the version badge was removed —
it rendered from git tags, and this repository has no tags yet, so it
permanently displayed "no tags" instead of the release-train version. The
authoritative version declaration is the `Version:` line below, which the
check-version-consistency gate asserts. -->

Consema 语言中立契约（RFC 0002/0003/0004/0006 契约家族；权威仓
`docs/rfcs/`）的 **Go 实现**仓库。本仓库是 Consema 六仓
拆分（2026-08-12 完成，见
[docs/six-repo-split-2026-08-12.md](https://github.com/consema/consema/blob/main/docs/six-repo-split-2026-08-12.md)）的 Go 仓：规范权威（RFC、docs、路线图、跨语言 conformance suites）在
[github.com/consema/consema](https://github.com/consema/consema)；本仓承载
Go 实现与跨语言差分验证工具。

Version: 1.0.0-rc.1（随 release train；`go.mod` 无版本声明，CI
check-version-consistency job 断言本行存在）。

## 快速开始（30 秒跑通）

```text
go get consema.dev/consema@latest
```

**发布前不可执行（G026，对抗审计 2026-08-14）：** `consema.dev` 域名
（DNS + vanity import meta）与模块发布均未就绪，且发布前须先完成
module 路径决策（`go.mod` 移至仓根使 module 保持
`consema.dev/consema`，或模块路径改为 `consema.dev/consema/go`）——
决策与域名均未完成前，该命令不会被 Go proxy 服务；下方 `go get` 路径
以发布时决策为准（见 [RELEASING.md](RELEASING.md) §1/§2）。当前在
仓库内运行：下方示例的入库副本在 [`go/examples/quickstart`](go/examples/quickstart)
（独立 `package main` 目录；CI examples job 编译运行它，并与下方代码栅栏
**逐字节比对**——R6，波 4 裁决 2026-08-15：旧机制「人工同步、不比对栅栏
文本」已证漂移并废弃），执行 `cd go && go run ./examples/quickstart`（一个
JSON 文档走完 parse → query → edit → render 四条链）：

```go
package main

import (
	"context"
	"fmt"
	"math/big"

	"consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
)

// member 是原生语义树成员查找（查询助手；完整操作符查询见 sdk_chain 示例）。
func member(value jsonpkg.JsonValue, name string) jsonpkg.JsonValue {
	members := value.ObjectMembers()
	if !members.IsAvailable() || members.Value() == nil {
		panic("not an object")
	}
	for _, m := range members.Value() {
		if n := m.Name(); n.IsAvailable() && n.Value() != nil && *n.Value() == name {
			return m.Value()
		}
	}
	panic("member not found")
}

func main() {
	ctx := context.Background()
	// 1. parse：json.strict 无损解析，Render() 与源字节逐字节一致
	parsed, err := consema.ParseDocument(ctx, []byte(`{"a":1,"b":{"c":2}}`),
		document.NewProfileId("json.strict", 1))
	if err != nil {
		panic(err)
	}
	jsonDoc, ok := parsed.AsJSON()
	if !ok {
		panic("not JSON")
	}
	// 2. query：原生语义树读 `b.c`
	c := member(member(jsonDoc.Root(), "b"), "c")
	// 3. edit：`b.c` 语义替换为 42（CanonicalForProfile），编辑外字节原样保留
	builder := jsonpkg.NewEditTransactionBuilder(jsonDoc)
	builder.SemanticScalar(c.NodeRef(), core.NewInteger(big.NewInt(42)),
		jsonpkg.RepresentationPolicyCanonicalForProfile)
	// Commit 返回 (*EditCommit, *EditFailure)：failure 必须用其自身类型
	// 判空（G053，对抗审计 2026-08-13）——若把 *EditFailure 复用进 error
	// 接口变量，成功时的 typed-nil 会被接口包装为非 nil，panic(err) 在
	// Error() 上对 nil 接收者解引用。
	commit, failure := jsonDoc.Commit(builder.Build())
	if failure != nil {
		panic(failure)
	}
	// 4. render：输出 `{"a":1,"b":{"c":42}}`
	fmt.Println(string(commit.Document.Render()))
}
```

完整链示例（parse → 操作符式原生语义查询 → best-exact 投影 → 结构编辑 → canonical 物化 → 跨格式转换到 TOML）：[`go/examples/sdk_chain`](go/examples/sdk_chain)，运行 `cd go && go run ./examples/sdk_chain`。上方快速开始代码的仓库副本在 [`go/examples/quickstart`](go/examples/quickstart)，与 README 栅栏**逐字节比对**（CI examples job 编译并运行该副本，并比对栅栏代码体与副本——R6，波 4 裁决 2026-08-15：补齐 kt 式 fence 比对门禁，旧机制「人工同步、不比对栅栏文本」已证漂移，G032/G053 注记见 ci-go.yml examples job）。

## API 摘要

核心面一行式（完整签名见 [go/README.md](go/README.md)；`Parse` / `Execute*Query` / `Project` / `Materialize` 在各家族包内，`Convert*` 为根级统一入口，G137）：

| 操作 | facade 入口 |
| --- | --- |
| parse | `consema.ParseDocument(ctx context.Context, source []byte, profile document.ProfileId) (*Document, error)` |
| query | `jsonpkg.ExecuteJSONQuery(ctx, executable *protocol.ExecutableQuery, doc *jsonpkg.Document, limits protocol.QueryLimits) ([]JsonMatch, *protocol.QueryFailure)` |
| project | `(*jsonpkg.Document).Project(request *jsonpkg.ProjectionRequest) ProjectionResult`（请求：`jsonpkg.NewProjectionRequestBuilder(jsonpkg.ProjectionTargetBestExactCoreV1).Build()`） |
| edit | `jsonpkg.NewEditTransactionBuilder(document)` + `(*jsonpkg.Document).Commit(tx *jsonpkg.EditTransaction) (*EditCommit, *EditFailure)`（`commit.Document` 为编辑后文档） |
| materialize | `jsonpkg.Materialize(value core.Value, request document.MaterializationRequest) MaterializationResult` |
| convert | `consema.ConvertJSON(source *jsonpkg.Document, request *jsonpkg.ProjectionRequest, materializationRequest document.MaterializationRequest) ConversionResult`（另有 ConvertTOML / ConvertYAML / ConvertINI / ConvertProperties / ConvertXML / ConvertPlist / ConvertHCL） |
| registry | `consema.Families()` / `consema.Profiles()` / `consema.QueryDomains()` / `consema.OperationRegistryFor(profile)`（8 家族 / 16 profiles / 21 查询域 / 16 操作注册表） |

## 布局

- `go/`：Go 模块（`go.mod`，module `consema.dev/consema`，go 1.26，RFC 0020
  §9.2 冻结）。完整文档见 [go/README.md](go/README.md)（全部里程碑
  0.14.0-0.19.0 G0.1-G5.7 已交付——G5.4 三平台验证中 macOS 腿 pending，
  见 go/README「Three-platform verification」，G028 口径：core / graph /
  protocol / document + 八格式家族 + CLI + 0.19.0 G5.7 real-repository
  pilot（go/pilot））。
- `scripts/`：跨语言差分验证脚本（byte parity / normalized differential /
  protocol exchange / shared conformance）。脚本构建 consema-rs 的 Rust
  emitter 并对拍 Go 实现；Rust 侧来自 consema-rs 仓 checkout（CI 多仓模式），
  conformance 数据来自规范仓 checkout。
- `.github/workflows/ci-go.yml`：7 个 job（G064，对抗审计 2026-08-14：
  旧文 6 个 job 漏 govulncheck）——go-matrix（1.26.0 声明最小版本 /
  1.26.5 矩阵钉定两版本 gofmt/vet/build/test/race 与 `-tags release`
  build/test 共 7 腿——R13，波 4 裁决 2026-08-15：`-tags release` 腿编译
  并测试 release 变体注入缝，与 go.mod 声明的
  `go 1.26` 最小版本真实对齐，RFC 0020 §9.2；R19，波 4 裁决
  2026-08-15：'当前稳定'腿按 RFC 0020 §9.2 从未满足，go.dev 当前
  stable 已是 1.26.6，矩阵升级 post-1.0.0）、coverage（≥60% 语句覆盖）、
  go-differential（windows-latest 多仓 checkout，四个跨语言差分
  harness）、govulncheck（Go 漏洞库审计；**每日 cron 在独立
  audit.yml**——本 job 仅 push/PR 触发，G099 波 4 2026-08-15 归因）、
  check-version-consistency、examples、check（alls-green 聚合门禁，
  branch protection 唯一必选 check）。

## 构建与测试

```text
cd go
go build ./...
go test ./...
go test -race ./...
```

前置（干净克隆必做）：conformance 数据不入 git（见 `.gitignore`），
`go test ./...` 的 conformance 套件用例（go/conformance，仓库相对路径
`conformance/vectors` 等）在未 provision 时直接失败；差分 harness 用例
（go/conformance/differential*）在 case 集不可达时 `t.Skipf` 跳过（G058，
对抗审计 2026-08-13：两条路径口径不同，均不会伪造成功）；**第三条路径
（R35，波 4 裁决 2026-08-15）**：`go/json`、`go/toml`、`go/yaml`、
`go/properties`、`go/pilot` 五个包的测试硬读 `../../conformance/...`
且无 skip 守卫（如 `go/json` 的 `TestJSON5ReferenceCorpus`），未
provision 时同样直接失败——三条路径中只有差分 harness 会跳过。
需要先按 [CONTRIBUTING.md](CONTRIBUTING.md)「Conformance 数据同步」并排
检出母仓 conformance 数据：从规范仓钉定 checkout 拷贝 `conformance/`
至本仓根、`docs/fc-manifest-0.13.0.json` 至 `docs/`（钉定 ref 与
ci-go.yml provision 步骤一致，R5 波 4 2026-08-15）。未 provision 时
conformance 套件测试会失败（非跳过），这正是 CI 多仓 checkout 模式所
provision 的内容。

## FAQ

- **支持哪些配置格式？** 八个格式家族、16 个 profiles：JSON（`json.strict@1` / `jsonc.bounded@1` / `json5.standard@1`）、TOML（`toml.1.0@1`）、YAML（`yaml.1.2-core@1` / `yaml.1.1-compat@1`）、INI（`ini.portable@1` / `ini.windows@1` / `ini.python-configparser@1`）、Java Properties（`java-properties.reader@1` / `java-properties.latin1@1`）、XML（`xml.1.0-safe@1`）、Property List（`plist.xml@1` / `plist.binary@1`）、HCL（`hcl.native@1` / `hcl.tfvars@1`）。完整面枚举见 `consema.Profiles()`。
- **与 encoding/json、gopkg.in/yaml.v3 等的关系？** 互不包装：Consema 是语言中立契约（RFC 0002/0003/0004/0006 契约家族；权威仓 `docs/rfcs/`）的独立 Go 实现，go.mod 零第三方依赖、纯标准库；JSON/YAML 等格式在 Consema 内是"格式内容处理面"（无损文档、查询、投影、原子编辑、跨格式转换），不是类型编解码。
- **性能如何？** 解析/渲染基准基线见 [go/README.md](go/README.md) 的 Benchmark 表（如 json parse 108 µs/op、render 1.45 µs/op）；Rust 侧权威基线见规范仓 `https://github.com/consema/consema/blob/main/docs/BENCHMARKS-0.13.0.md`。
- **零依赖吗？** 是——`go.mod` 零 `require`，只使用标准库（math/big、hash/fnv、crypto/sha256、unicode/utf8 等）。
- **跨语言一致性如何保证？** 18 套语言无关 conformance suite 共 519/519 cases（聚合 digest `cfd6e296…`）由规范仓维护，四个 runner 钉定/复算（Rust vendored DIGEST / Go VerifyVectorsDigest / Python AGGREGATE_SHA256 / Kotlin ci-kotlin.yml 字面量；TS 不钉定聚合 digest——其断言仅在 provisioned manifest 存在时执行，而 ts CI 刻意不 provision——wave-5 P2 如实披露）；CI 多仓 checkout 跑 conformance runner 与四个 Go-Rust 差分门禁（byte parity / normalized differential / protocol exchange / shared conformance；G029，对抗审计 2026-08-14：旧文只列三个 harness，漏 shared-conformance）。
- **兼容承诺？** 语义化版本（release train，模块版本来自 tag）；`check-version-consistency` 门禁断言 README 版本行存在；`go-matrix` 门禁在声明的最小 Go 版本（1.26，RFC 0020 §9.2 冻结）与矩阵钉定的 1.26.5 上真实验证（R19，波 4 裁决 2026-08-15：'当前稳定'腿从未满足——go.dev 当前 stable 已是 1.26.6，矩阵升级 post-1.0.0）；兼容与支持政策见 RFC 0020。
- **如何贡献？** 见本仓 [CONTRIBUTING.md](CONTRIBUTING.md)（规范仓为权威版）；conformance 向量/夹具/oracle/差分数据权威在规范仓——向量变更是五仓同步事件，必须先回规范仓提交再同步五个语言仓。
- **"默认拒绝信息损失"是什么意思？** 投影/转换/编辑中的任何 loss（如 YAML 共享结构展开、Properties 重复键折叠、数值舍入）必须显式授权；未授权时操作原子失败（`ConversionResult.Failed`；fidelity 三档：Exact / Transformed / Lossy）。

## 六仓导航

| 仓库 | 角色 |
| --- | --- |
| [consema](https://github.com/consema/consema) | 规范 / RFC / 路线图 / 审计证据 / conformance 仲裁层（语言无关权威） |
| [consema-rs](https://github.com/consema/consema-rs) | Rust 参考实现 |
| [consema-go](https://github.com/consema/consema-go)（本仓） | Go 实现 |
| [consema-ts](https://github.com/consema/consema-ts) | TypeScript 实现 |
| [consema-py](https://github.com/consema/consema-py) | Python 实现 |
| [consema-kt](https://github.com/consema/consema-kt) | Kotlin 实现 |

## 文档导航

- 规范仓（RFC / docs / 路线图 / conformance 权威）：https://github.com/consema/consema
- [RFC 0001-0016](https://github.com/consema/consema/tree/main/docs/rfcs) + [RFC 0020 兼容与支持政策](https://github.com/consema/consema/blob/main/docs/rfcs/0020-compatibility-and-support-policy-v1.md)：语言无关规范的权威载体
- [1.0.0 产品路线图](https://github.com/consema/consema/blob/main/Consema%201.0.0%20产品路线图与双语言落地设计.md)
- [平台接入指南](https://github.com/consema/consema/blob/main/docs/platform-integration-guide.md)
- [CLI Cookbook（可复制配方）](https://github.com/consema/consema/blob/main/docs/cookbook.md)
- [多语言实现计划](https://github.com/consema/consema/blob/main/docs/multi-language-implementation-plan.md) / [五语言 CI 设计](https://github.com/consema/consema/blob/main/docs/five-language-ci-design.md)
