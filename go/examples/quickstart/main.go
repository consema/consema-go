// Command quickstart is the repository copy of the root README quick-start
// example (README.md "快速开始（30 秒跑通）"): one JSON document through
// parse -> query -> edit -> render. The CI examples job compiles and runs
// it (go run ./examples/quickstart must exit 0), which keeps the documented
// example honest (G053, adversarial audit 2026-08-13: the documented
// example previously reused the error interface for Commit's *EditFailure
// and panicked on the typed-nil trap). Keep this file in sync with the
// README snippet by hand.
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
	// 判空（G053）——若把 *EditFailure 复用进 error 接口变量，成功时的
	// typed-nil 会被接口包装为非 nil，panic(err) 在 Error() 上对 nil
	// 接收者解引用。
	commit, failure := jsonDoc.Commit(builder.Build())
	if failure != nil {
		panic(failure)
	}
	// 4. render：输出 `{"a":1,"b":{"c":42}}`
	fmt.Println(string(commit.Document.Render()))
}
