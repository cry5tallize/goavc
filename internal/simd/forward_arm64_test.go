//go:build arm64 && !purego

package simd

import "testing"

func TestForward_NEON(t *testing.T)             { checkForward(t, forward4NEON) }
func BenchmarkForward4x4_4x4_NEON(b *testing.B) { benchForward(b, forward4NEON) }
