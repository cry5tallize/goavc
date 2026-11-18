package codeccli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	h264 "github.com/cry5tallize/goavc"
)

func testYUV(frames int) []byte {
	data := make([]byte, frames*16*16*3/2)
	for i := range data {
		// Baseline/Main的PCM合法采样域为1..255；零采样拒绝另有codec回归。
		data[i] = byte(1 + (i*13+i/384*7)%255)
	}
	return data
}

func encodeArgs(extra ...string) []string {
	return append([]string{"-width", "16", "-height", "16", "-progress", "0"}, extra...)
}

func TestCodecCLI_SmallSequence(t *testing.T) {
	for _, format := range []string{"annexb", "avcc"} {
		t.Run(format, func(t *testing.T) {
			raw := testYUV(3)
			var coded, decoded, log bytes.Buffer
			args := encodeArgs("-pcm", "-format", format)
			decArgs := []string{"-format", format, "-progress", "0"}
			if format == "avcc" {
				args = append(args, "-length-size", "2")
				decArgs = append(decArgs, "-length-size", "2")
			}
			if err := Encode(t.Context(), args, bytes.NewReader(raw), &coded, &log); err != nil {
				t.Fatal(err)
			}
			if coded.Len() == 0 || log.Len() != 0 {
				t.Fatalf("码流=%d，关闭进度后的日志=%q", coded.Len(), log.String())
			}
			if err := Decode(t.Context(), decArgs, &coded, &decoded, &log); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, decoded.Bytes()) {
				t.Fatalf("PCM 往返不一致：输入%d，输出%d", len(raw), decoded.Len())
			}
		})
	}
}

func TestCodecCLI_FilesAndOwnedResources(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.yuv")
	coded := filepath.Join(dir, "coded.h264")
	renamed := filepath.Join(dir, "closed.h264")
	output := filepath.Join(dir, "decoded.yuv")
	raw := testYUV(2)
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Encode(t.Context(), encodeArgs("-pcm", "-input", input, "-output", coded), bytes.NewReader(nil), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	// Windows 上未关闭的 owned 文件会阻止重命名；同时检查输出可独立重新打开。
	if err := os.Rename(coded, renamed); err != nil {
		t.Fatalf("编码器未释放输出：%v", err)
	}
	if err := os.Rename(input, input+".closed"); err != nil {
		t.Fatalf("编码器未释放输入：%v", err)
	}
	if err := Decode(t.Context(), []string{"-input", renamed, "-output", output, "-progress", "0"}, bytes.NewReader(nil), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(renamed, coded); err != nil {
		t.Fatalf("解码器未释放输入：%v", err)
	}
	if err := os.Rename(output, output+".closed"); err != nil {
		t.Fatalf("解码器未释放输出：%v", err)
	}
	got, err := os.ReadFile(output + ".closed")
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("文件编解码失败：%v，字节%d", err, len(got))
	}
}

func TestCodecCLI_EncoderToolsAndProgress(t *testing.T) {
	var coded, decoded, log bytes.Buffer
	args := encodeArgs("-profile", "high", "-qp", "18", "-gop", "4", "-idr", "4",
		"-bframes", "1", "-fps", "30000/1001", "-lookahead", "2", "-8x8", "-weighted", "-progress", "1ns")
	if err := Encode(t.Context(), args, bytes.NewReader(testYUV(5)), &coded, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "完成 frames=5") || !strings.Contains(log.String(), "bytes=") {
		t.Fatalf("缺少 stderr 进度：%q", log.String())
	}
	log.Reset()
	if err := Decode(t.Context(), []string{"-progress", "1ns"}, &coded, &decoded, &log); err != nil {
		t.Fatal(err)
	}
	if decoded.Len() != len(testYUV(5)) || !strings.Contains(log.String(), "完成 frames=5") {
		t.Fatalf("重排或排空失败：字节%d，日志%q", decoded.Len(), log.String())
	}
}

func TestCodecCLI_RateParameters(t *testing.T) {
	for _, rate := range []string{"cqp", "abr", "cbr", "vbv"} {
		t.Run(rate, func(t *testing.T) {
			var coded bytes.Buffer
			args := encodeArgs("-rate", rate, "-bitrate", "100000", "-max-bitrate", "200000", "-vbv-buffer-size", "100000")
			if err := Encode(t.Context(), args, bytes.NewReader(testYUV(2)), &coded, io.Discard); err != nil {
				t.Fatal(err)
			}
			if coded.Len() == 0 {
				t.Fatal("未输出码流")
			}
		})
	}
}

func TestCodecCLI_InvalidConfigurationPreservesOutput(t *testing.T) {
	encCases := [][]string{
		{"-width", "0"}, {"-height", "17"}, {"-qp", "52"}, {"-gop", "0"}, {"-idr", "-1"},
		{"-bframes", "1"}, {"-rate", "unknown"}, {"-rate", "abr"}, {"-fps", "1/0"}, {"-fps", "1/2/3"},
		{"-slices", "2"}, {"-lookahead", "17"}, {"-lookahead", "-1"}, {"-8x8"}, {"-weighted"},
		{"-pixel-format", "yuv422p"}, {"-profile", "high10"}, {"-threads", "2"},
		{"-field-order", "unknown"}, {"-field-order", "tff"},
		{"-profile", "high", "-height", "34", "-field-order", "bff"},
		{"-profile", "high", "-height", "32", "-slices", "2", "-field-order", "tff"},
		{"-format", "mp4"}, {"-format", "avcc", "-length-size", "0"}, {"-length-size", "2"},
		{"-max-input-buffer", "0"}, {"-max-input-buffer", "383"}, {"-max-nal-size", "0"},
		{"-max-au-size", "-1"}, {"-max-nal-units", "0"}, {"-progress", "-1s"}, {"unexpected"},
	}
	decCases := [][]string{
		{"-max-width", "0"}, {"-max-height", "9000"}, {"-max-frame-size", "-1"},
		{"-max-slices", "0"}, {"-max-dpb-frames", "0"}, {"-threads", "-1"},
		{"-pixel-format", "rgb24"}, {"-format", "auto"}, {"-max-input-buffer", "-1"},
	}
	for kind, cases := range map[string][][]string{"encode": encCases, "decode": decCases} {
		for _, args := range cases {
			t.Run(kind+"/"+strings.Join(args, "_"), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "keep.bin")
				original := []byte("不得截断已有输出")
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "encode" {
					a := append(encodeArgs("-output", path), args...)
					err = Encode(t.Context(), a, bytes.NewReader(nil), io.Discard, io.Discard)
				} else {
					a := append([]string{"-output", path, "-progress", "0"}, args...)
					err = Decode(t.Context(), a, bytes.NewReader(nil), io.Discard, io.Discard)
				}
				if err == nil {
					t.Fatal("非法参数被接受")
				}
				got, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(got, original) {
					t.Fatalf("输出已改变：%q，%v", got, readErr)
				}
			})
		}
	}
}

func TestCodecCLI_SameFileAliases(t *testing.T) {
	for _, alias := range []string{"same", "relative", "hardlink", "symlink", "stdout"} {
		t.Run(alias, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.yuv")
			raw := testYUV(1)
			if err := os.WriteFile(input, raw, 0600); err != nil {
				t.Fatal(err)
			}
			output := input
			var stdout io.Writer = io.Discard
			switch alias {
			case "relative":
				var err error
				cwd, e := os.Getwd()
				if e != nil {
					t.Fatal(e)
				}
				output, err = filepath.Rel(cwd, input)
				if err != nil {
					t.Skip(err)
				}
			case "hardlink", "symlink":
				output = filepath.Join(dir, "alias.yuv")
				link := os.Link
				if alias == "symlink" {
					link = os.Symlink
				}
				if err := link(input, output); err != nil {
					t.Skipf("环境不允许创建链接：%v", err)
				}
			case "stdout":
				f, err := os.OpenFile(input, os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				stdout, output = f, "-"
			}
			err := Encode(t.Context(), encodeArgs("-input", input, "-output", output), bytes.NewReader(nil), stdout, io.Discard)
			if !errors.Is(err, h264.ErrInvalidData) {
				t.Fatalf("未拒绝同一对象：%v", err)
			}
			got, err := os.ReadFile(input)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatalf("源文件已损坏：%v", err)
			}
		})
	}
}

func TestCodecCLI_TruncationAndResourceLimits(t *testing.T) {
	if err := Encode(t.Context(), encodeArgs(), bytes.NewReader(testYUV(1)[:383]), io.Discard, io.Discard); !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("YUV 截断错误：%v", err)
	}
	var avcc, annexb bytes.Buffer
	if err := Encode(t.Context(), encodeArgs("-pcm", "-format", "avcc"), bytes.NewReader(testYUV(1)), &avcc, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Decode(t.Context(), []string{"-format", "avcc", "-progress", "0"}, bytes.NewReader(avcc.Bytes()[:avcc.Len()-1]), io.Discard, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("AVCC 截断错误：%v", err)
	}
	if err := Encode(t.Context(), encodeArgs("-pcm"), bytes.NewReader(testYUV(1)), &annexb, io.Discard); err != nil {
		t.Fatal(err)
	}
	broken := append(append([]byte(nil), annexb.Bytes()...), 0, 0, 1)
	if err := Decode(t.Context(), []string{"-progress", "0"}, bytes.NewReader(broken), io.Discard, io.Discard); !errors.Is(err, h264.ErrInvalidData) {
		t.Fatalf("Annex-B 尾起始码被误报 EOF：%v", err)
	}
	for _, args := range [][]string{
		{"-max-nal-size", "16"}, {"-max-input-buffer", "32"}, {"-max-au-size", "32"},
		{"-max-frame-size", "1"},
	} {
		if err := Decode(t.Context(), append(args, "-progress", "0"), bytes.NewReader(annexb.Bytes()), io.Discard, io.Discard); !errors.Is(err, h264.ErrResourceLimit) {
			t.Fatalf("资源上限%v未生效：%v", args, err)
		}
	}
}

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestCodecCLI_WriteErrors(t *testing.T) {
	sentinel := errors.New("测试写失败")
	var coded bytes.Buffer
	if err := Encode(t.Context(), encodeArgs("-pcm"), bytes.NewReader(testYUV(1)), &coded, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		writer io.Writer
		want   error
	}{{"error", failWriter{sentinel}, sentinel}, {"short", shortWriter{}, io.ErrShortWrite}} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Encode(t.Context(), encodeArgs("-pcm"), bytes.NewReader(testYUV(1)), tc.writer, io.Discard); !errors.Is(err, tc.want) {
				t.Fatalf("编码写错误丢失：%v", err)
			}
			if err := Decode(t.Context(), []string{"-progress", "0"}, bytes.NewReader(coded.Bytes()), tc.writer, io.Discard); !errors.Is(err, tc.want) {
				t.Fatalf("解码写错误丢失：%v", err)
			}
		})
	}
}

func TestCodecCLI_OwnedCancellation(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owned := newOwnedIO(ctx)
	owned.add(r)
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		var b [1]byte
		_, err := r.Read(b[:])
		result <- err
	}()
	<-started
	cancel()
	if err := owned.finish(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("关闭 owned I/O 未中断读取")
		}
	case <-time.After(5 * time.Second):
		_ = w.Close()
		t.Fatal("取消后 owned 读取仍阻塞")
	}
	// 回调结束后追加的文件必须立即关闭；正常清理不能再次关闭同一对象。
	extra, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	owned.add(extra)
	if _, err := extra.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("取消后新资源泄漏：%v", err)
	}
}

type startedReader struct {
	src     io.Reader
	started chan struct{}
	once    sync.Once
}

func (r *startedReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.src.Read(p)
}

func TestCodecCLI_BorrowedCancellationPolicy(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := &startedReader{src: r, started: make(chan struct{})}
	result := make(chan error, 1)
	go func() { result <- Encode(ctx, encodeArgs("-pcm"), input, io.Discard, io.Discard) }()
	<-input.started
	cancel()
	// 借用 stdin 不会被取消回调关闭；写入完整帧解除阻塞后才返回取消。
	if _, err := w.Write(testYUV(1)); err != nil {
		t.Fatalf("借用输入被关闭：%v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消原因丢失：%v", err)
		}
	case <-time.After(5 * time.Second):
		_ = r.Close()
		t.Fatal("解除借用 I/O 阻塞后未返回")
	}
}

func TestCodecCLI_CanceledPreservesOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep.yuv")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Encode(ctx, encodeArgs("-output", path), bytes.NewReader(testYUV(1)), io.Discard, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "keep" {
		t.Fatalf("已取消命令截断输出：%q，%v", got, err)
	}
}

// 使用当前测试可执行文件运行真正的进程入口，不在测试中调用 go build。
func TestCodecCLI_SubprocessHelper(t *testing.T) {
	mode := os.Getenv("GOAVC_CLI_HELPER")
	if mode == "" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"h264" + mode}, os.Args[i+1:]...)
			Main(mode == "enc")
			os.Exit(0)
		}
	}
	os.Exit(2)
}

func helperCommand(ctx context.Context, mode string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCodecCLI_SubprocessHelper$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "GOAVC_CLI_HELPER="+mode)
	return cmd
}

func TestCodecCLI_BinaryPipe(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, format := range []string{"annexb", "avcc"} {
		t.Run(format, func(t *testing.T) {
			raw := testYUV(3)
			enc := helperCommand(ctx, "enc", encodeArgs("-pcm", "-format", format, "-progress", "1ns")...)
			dec := helperCommand(ctx, "dec", "-format", format, "-progress", "1ns")
			var decoded, encLog, decLog bytes.Buffer
			enc.Stdin, enc.Stderr = bytes.NewReader(raw), &encLog
			dec.Stdout, dec.Stderr = &decoded, &decLog
			pipe, err := enc.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer pipe.Close()
			dec.Stdin = pipe
			if err := enc.Start(); err != nil {
				t.Fatal(err)
			}
			if err := dec.Start(); err != nil {
				_ = enc.Process.Kill()
				_ = enc.Wait()
				t.Fatal(err)
			}
			// 必须让下游读到 EOF 后再 Wait 上游，避免 StdoutPipe 被提前关闭。
			decErr := dec.Wait()
			encErr := enc.Wait()
			if encErr != nil || decErr != nil {
				t.Fatalf("二进制管道失败：enc=%v %s；dec=%v %s", encErr, encLog.String(), decErr, decLog.String())
			}
			if !bytes.Equal(raw, decoded.Bytes()) || !strings.Contains(encLog.String(), "frames=3") || !strings.Contains(decLog.String(), "frames=3") {
				t.Fatalf("stdout 被日志污染或帧丢失：输出%d，编码日志%q，解码日志%q", decoded.Len(), encLog.String(), decLog.String())
			}
		})
	}
}

func TestCodecCLI_ErrorExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := helperCommand(ctx, "enc", "-width", "0", "-height", "16")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("错误退出契约：err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}
