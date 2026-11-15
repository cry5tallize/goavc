// h264enc 将紧密平面 YUV 编码为 Annex-B 或 AVCC 裸流。
package main

import "github.com/cry5tallize/goavc/cmd/internal/codeccli"

func main() {
	codeccli.Main(true)
}
