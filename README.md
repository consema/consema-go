# Consema Go SDK（consema-go）

![CI](https://img.shields.io/github/actions/workflow/status/consema/consema-go/ci-go.yml?branch=main)
![Version](https://img.shields.io/github/v/tag/consema/consema-go)
![License](https://img.shields.io/github/license/consema/consema-go)

Consema 语言中立契约（RFC 0016）的 **Go 实现**仓库。本仓库是 Consema 六仓
拆分中的 Go 仓：规范权威（RFC、docs、路线图、跨语言 conformance suites）在
[github.com/consema/consema](https://github.com/consema/consema)；本仓承载
Go 实现与跨语言差分验证工具。

Version: 1.0.0-rc.1（随 release train；`go.mod` 无版本声明，CI
check-version-consistency job 断言本行存在）。

## 快速开始（30 秒跑通）

```text
go get consema.dev/consema@latest
```

把下面代码放进任意包（一个 JSON 文档走完 parse → query → edit → render 四条链）：

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
	commit, err := jsonDoc.Commit(builder.Build())
	if err != nil {
		panic(err)
	}
	// 4. render：输出 `{"a":1,"b":{"c":42}}`
	fmt.Println(string(commit.Document.Render()))
}
```

完整链示例（parse → 操作符式原生语义查询 → best-exact 投影 → 结构编辑 → canonical 物化 → 跨格式转换到 TOML）：[`go/examples/sdk_chain`](go/examples/sdk_chain)，运行 `cd go && go run ./examples/sdk_chain`。

## API 摘要

核心面一行式（完整签名见 [go/README.md](go/README.md)；八个格式家族各有独立的 `Parse` / `Execute*Query` / `Project` / `Materialize` / `Convert*` 入口）：

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

- `go/`：Go 模块（`go.mod`，module `consema.dev/consema`，go 1.24）。完整文档
  见 [go/README.md](go/README.md)（全部里程碑 0.14.0-0.19.0 G0.1-G5.6 已交付：
  core / graph / protocol / document + 八格式家族 + CLI）。
- `scripts/`：跨语言差分验证脚本（byte parity / normalized differential /
  protocol exchange / shared conformance）。脚本构建 consema-rs 的 Rust
  emitter 并对拍 Go 实现；Rust 侧来自 consema-rs 仓 checkout（CI 多仓模式），
  conformance 数据来自规范仓 checkout。
- `.github/workflows/ci-go.yml`：Go 门禁（go-matrix：1.24.x / 1.25.x /
  1.26.5 三版本 gofmt/vet/build/test/race，与 go.mod 声明的 `go 1.24` 最小
  版本真实对齐）与 Go-Rust 差分门禁（windows-latest 多仓 checkout）。

## 构建与测试

```text
cd go
go build ./...
go test ./...
go test -race ./...
```

前置（干净克隆必做）：conformance 数据不入 git（见 `.gitignore`），
`go test ./...` 的 conformance 用例（go/conformance，仓库相对路径
`conformance/vectors` 等，无 skip 直接失败）需要先按
[CONTRIBUTING.md](CONTRIBUTING.md)「Conformance 数据同步」并排检出母仓
conformance 数据：从规范仓拷贝 `conformance/` 至本仓根、
`docs/fc-manifest-0.13.0.json` 至 `docs/`。未 provision 时 conformance
测试会失败（非跳过），这正是 CI 多仓 checkout 模式所 provision 的内容。

## FAQ

- **支持哪些配置格式？** 八个格式家族、16 个 profiles：JSON（`json.strict@1` / `jsonc.bounded@1` / `json5.standard@1`）、TOML（`toml.1.0@1`）、YAML（`yaml.1.2-core@1` / `yaml.1.1-compat@1`）、INI（`ini.portable@1` / `ini.windows@1` / `ini.python-configparser@1`）、Java Properties（`java-properties.reader@1` / `java-properties.latin1@1`）、XML（`xml.1.0-safe@1`）、Property List（`plist.xml@1` / `plist.binary@1`）、HCL（`hcl.native@1` / `hcl.tfvars@1`）。完整面枚举见 `consema.Profiles()`。
- **与 encoding/json、gopkg.in/yaml.v3 等的关系？** 互不包装：Consema 是语言中立契约（RFC 0016）的独立 Go 实现，go.mod 零第三方依赖、纯标准库；JSON/YAML 等格式在 Consema 内是"格式内容处理面"（无损文档、查询、投影、原子编辑、跨格式转换），不是类型编解码。
- **性能如何？** 解析/渲染基准基线见 [go/README.md](go/README.md) 的 Benchmark 表（如 json parse 108 µs/op、render 1.45 µs/op）；Rust 侧权威基线见规范仓 `docs/BENCHMARKS-0.13.0.md`。
- **零依赖吗？** 是——`go.mod` 零 `require`，只使用标准库（math/big、hash/fnv、crypto/sha256、unicode/utf8 等）。
- **跨语言一致性如何保证？** 18 套语言无关 conformance suite 共 519/519 cases（聚合 digest `cfd6e296…`）由规范仓维护、五仓共享；CI 多仓 checkout 跑 conformance runner 与 Go-Rust 差分门禁（byte parity / normalized differential / protocol-exchange）。
- **兼容承诺？** 语义化版本（release train，模块版本来自 tag）；`check-version-consistency` 门禁断言 README 版本行存在；`go-matrix` 门禁在声明的最小 Go 版本（1.24）上真实验证；兼容与支持政策见 RFC 0020。
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
