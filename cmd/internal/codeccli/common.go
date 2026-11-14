// Package codeccli 提供两个编解码命令的参数、资源所有权和进度管理。
package codeccli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	h264 "github.com/goavc/h264"
)

// Main将信号转成取消。独立命令拥有进程标准流，首次信号关闭stdin/stdout以
// 中断管道I/O；库式Encode/Decode仍不关闭调用方传入的借用流。
func Main(encode bool) {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-signals:
			signal.Stop(signals)
			cancel()
			_ = os.Stdin.Close()
			_ = os.Stdout.Close()
		case <-ctx.Done():
		}
	}()
	var err error
	if encode {
		err = Encode(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	} else {
		err = Decode(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	}
	signal.Stop(signals)
	cancel()
	<-done
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type common struct {
	input, output, format      string
	lengthSize, maxInput       int
	maxNAL, maxAU, maxNALUnits int
	progress                   time.Duration
}

func (c *common) flags(f *flag.FlagSet) {
	f.StringVar(&c.input, "input", "-", "输入文件；- 为 stdin")
	f.StringVar(&c.output, "output", "-", "输出文件；- 为 stdout")
	f.StringVar(&c.format, "format", "annexb", "H.264 裸流封装：annexb 或 avcc；不含 MP4/avcC")
	f.IntVar(&c.lengthSize, "length-size", 4, "AVCC 大端长度前缀字节数，1..4；Annex-B 只允许默认4")
	f.IntVar(&c.maxInput, "max-input-buffer", 16<<20, "pipeline 单个输入的字节上限；不限制 codec 内部缓存或进程 RSS")
	f.IntVar(&c.maxNAL, "max-nal-size", 8<<20, "单个 NAL 字节上限（含 header）")
	f.IntVar(&c.maxAU, "max-au-size", 32<<20, "单个 AU 字节上限（含起始码）")
	f.IntVar(&c.maxNALUnits, "max-nal-units", 4096, "单个 AU 的 NAL 数量上限")
	f.DurationVar(&c.progress, "progress", time.Second, "stderr 进度间隔；0 关闭；仅在完成输出时刷新")
	f.Usage = func() {
		fmt.Fprintf(f.Output(), "用法：%s [选项]\n所有日志仅写 stderr；文件输出会覆盖原内容。\n独立命令收到首个取消信号会关闭文件及进程stdin/stdout以中断管道；库式调用不关闭传入的借用流。\n", f.Name())
		f.PrintDefaults()
	}
}

func (c common) validate(f *flag.FlagSet) error {
	if f.NArg() != 0 || c.input == "" || c.output == "" ||
		(c.format != "annexb" && c.format != "avcc") || c.lengthSize < 1 || c.lengthSize > 4 ||
		(c.format == "annexb" && c.lengthSize != 4) || c.maxInput <= 0 || c.maxNAL <= 0 ||
		c.maxAU <= 0 || c.maxNALUnits <= 0 || c.progress < 0 {
		return fmt.Errorf("%s: 路径、封装或资源上限无效: %w", f.Name(), h264.ErrInvalidData)
	}
	return nil
}

func pixelFormat(name string) (h264.PixelFormat, error) {
	switch name {
	case "yuv420p":
		return h264.YUV420P, nil
	case "yuv422p":
		return h264.YUV422P, nil
	case "yuv444p":
		return h264.YUV444P, nil
	default:
		return 0, fmt.Errorf("不支持的像素格式 %q: %w", name, h264.ErrUnsupported)
	}
}

// ownedIO 不关闭借用流。AfterFunc 与普通清理共享一次性关闭，并在返回前等待回调结束。
// 不启动代读/代写 goroutine，因而借用流的阻塞操作不会因取消而遗留后台工作。
type ownedIO struct {
	mu       sync.Mutex
	files    []*os.File
	closed   bool
	closeErr error
	stop     func() bool
	done     chan struct{}
}

func newOwnedIO(ctx context.Context) *ownedIO {
	o := &ownedIO{done: make(chan struct{})}
	o.stop = context.AfterFunc(ctx, func() {
		o.close()
		close(o.done)
	})
	return o
}

func (o *ownedIO) add(f *os.File) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		_ = f.Close()
	} else {
		o.files = append(o.files, f)
	}
}

func (o *ownedIO) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	o.closed = true
	for i := len(o.files) - 1; i >= 0; i-- {
		o.closeErr = errors.Join(o.closeErr, o.files[i].Close())
	}
}

func (o *ownedIO) finish() error {
	if !o.stop() {
		<-o.done
	}
	o.close()
	return o.closeErr
}

// open 不使用 O_TRUNC；先比较已打开的对象，再截断，以拒绝相对路径、符号链接及硬链接别名。
func (o *ownedIO) open(ctx context.Context, c common, stdin io.Reader, stdout io.Writer) (io.Reader, io.Writer, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	in := stdin
	if c.input != "-" {
		f, err := os.Open(c.input)
		if err != nil {
			return nil, nil, err
		}
		o.add(f)
		in = f
	}
	out := stdout
	var output *os.File
	if c.output != "-" {
		f, err := os.OpenFile(c.output, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			return nil, nil, err
		}
		o.add(f)
		out, output = f, f
	}
	if a, ok := in.(*os.File); ok {
		if b, ok := out.(*os.File); ok {
			ai, err := a.Stat()
			if err != nil {
				return nil, nil, err
			}
			bi, err := b.Stat()
			if err != nil {
				return nil, nil, err
			}
			// Windows匿名管道的FileInfo没有稳定文件索引；SameFile只能用于普通文件，
			// 否则两个不同的标准流可能被误判为同一对象。
			if a == b || ai.Mode().IsRegular() && bi.Mode().IsRegular() && os.SameFile(ai, bi) {
				return nil, nil, fmt.Errorf("输入输出指向同一对象: %w", h264.ErrInvalidData)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if output != nil {
		info, err := output.Stat()
		if err != nil {
			return nil, nil, err
		}
		if info.Mode().IsRegular() {
			if err := output.Truncate(0); err != nil {
				return nil, nil, err
			}
		}
	}
	return in, out, nil
}

type progress struct {
	out           io.Writer
	interval      time.Duration
	start, last   time.Time
	frames, bytes uint64
}

func newProgress(out io.Writer, interval time.Duration) *progress {
	now := time.Now()
	return &progress{out: out, interval: interval, start: now, last: now}
}

func (p *progress) advance() {
	p.frames++
	if p.interval > 0 && time.Since(p.last) >= p.interval {
		p.report("处理中")
	}
}

func (p *progress) report(state string) {
	if p.interval == 0 {
		return
	}
	now := time.Now()
	fmt.Fprintf(p.out, "%s frames=%d bytes=%d elapsed=%s\n", state, p.frames, p.bytes, now.Sub(p.start).Round(time.Millisecond))
	p.last = now
}

type countedWriter struct {
	dst io.Writer
	p   *progress
}

func (w countedWriter) Write(data []byte) (int, error) {
	n, err := w.dst.Write(data)
	if n > 0 && n <= len(data) {
		w.p.bytes += uint64(n)
	}
	return n, err
}

func finish(ctx context.Context, owned *ownedIO, p *progress, err *error) {
	closeErr := owned.finish()
	if ctx.Err() != nil {
		*err = ctx.Err()
	} else {
		if *err == io.EOF {
			*err = nil
		}
		*err = errors.Join(*err, closeErr)
	}
	if *err != nil {
		p.report("失败")
	} else {
		p.report("完成")
	}
}
