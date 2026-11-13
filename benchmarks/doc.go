// Package benchmarks 汇总端到端与内核级基准测试。
//
// 为什么基准单独成包：基准需要复用各层的构造代码，若散落在功能包内，
// 一是会污染包的依赖（例如基准引入 testdata 加载器），二是无法用
// `go test -bench . ./benchmarks` 一次性跑完全部对比。
//
// 记录规范见 docs/performance.md：每条结果必须附硬件、commit、参数与
// 输入语料哈希，否则数字不可比。
package benchmarks
