//go:build amd64 && !purego

package simd

// 下列入口不保留指针、不分配内存。每行按完整向量、窄加载及标量尾部
// 读取，最大读取位置为width-1，行尾无需padding，不要求基址对齐。
// uint16有效域0..1023，差值先按unsigned求绝对值，再扩为64位累加。
//
//go:noescape
func sad8SSE2(a []byte, as int, b []byte, bs, width, height int) int

//go:noescape
func sad16SSE2(a []uint16, as int, b []uint16, bs, width, height int) int
