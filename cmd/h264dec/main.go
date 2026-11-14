// h264dec 将 Annex-B 或 AVCC 裸流解码为紧密平面 YUV。
package main

import "github.com/goavc/h264/cmd/internal/codeccli"

func main() {
	codeccli.Main(false)
}
