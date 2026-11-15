// h264dec 将 Annex-B 或 AVCC 裸流解码为紧密平面 YUV。
package main

import "github.com/cry5tallize/goavc/cmd/internal/codeccli"

func main() {
	codeccli.Main(false)
}
