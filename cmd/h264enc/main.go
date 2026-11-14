// h264enc 将紧密平面 YUV 编码为 Annex-B 或 AVCC 裸流。
package main

import "github.com/goavc/h264/cmd/internal/codeccli"

func main() {
	codeccli.Main(true)
}
