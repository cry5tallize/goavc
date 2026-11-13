// Package importgraph 解析 Go 源码的 import 关系，用于强制
// docs/architecture.md 中的依赖规则（R1/R2/R3/R5）。
//
// 为什么不直接用 go list：这里需要在"包还无法编译"的阶段也能检查
// 依赖方向（例如骨架刚建好、包里只有 doc.go 时）。纯 AST 解析不受
// 编译状态影响，能在 CI 更早的阶段拦住错误的依赖。
package importgraph
