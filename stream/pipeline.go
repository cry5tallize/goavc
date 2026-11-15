package stream

import (
	"context"
	"errors"
	"fmt"
	"io"

	h264 "github.com/cry5tallize/goavc"
	"github.com/cry5tallize/goavc/decoder"
	"github.com/cry5tallize/goavc/encoder"
)

// DefaultMaxInputBuffer是pipeline单个输入载荷的默认上限（两倍默认NAL上限）。
// 不包含调用方源的缓存、NAL扫描器、codec参考帧/Lookahead或输出Writer缓存；
// 这些各自受适配器和codec配置约束，并非整个进程的RSS上限。
const DefaultMaxInputBuffer = 16 << 20

// EncodeConfig配置一次编码流；pipeline拥有内部Encoder，任何退出均Close。
// MaxInputBuffer限制输入三平面Data长度总和（含stride），0采用默认值。
// Options与encoder.New相同，配置中的slice不在调用期间修改。
type EncodeConfig struct {
	Encoder        h264.EncoderConfig
	Options        []encoder.Option
	MaxInputBuffer int
}

// DecodeConfig配置一次解码流；MaxInputBuffer限制完整Annex-B输入AU的字节数。
// 零值使用默认预算；Decoder配置继续约束NAL、图像和DPB。
type DecodeConfig struct {
	Decoder        h264.DecoderConfig
	MaxInputBuffer int
}

// ContextFrameReader在可能阻塞的端点处实现可取消读取。成功帧的所有权与
// FrameReader相同；取消必须返回ctx.Err且不得遗留后台I/O。pipeline优先调用它。
type ContextFrameReader interface {
	ReadFrameContext(context.Context) (*h264.Frame, error)
}

// ContextFrameWriter在可能阻塞的端点处同步响应取消，不得保留帧引用。
type ContextFrameWriter interface {
	WriteFrameContext(context.Context, *h264.Frame) error
}

// ContextAccessUnitReader是可取消的完整AU源，成功Packet独立归调用方。
type ContextAccessUnitReader interface {
	ReadAccessUnitContext(context.Context) (h264.Packet, error)
}

// ContextAccessUnitWriter是可取消的AU目标，返回时不再保留Packet.Data。
type ContextAccessUnitWriter interface {
	WriteAccessUnitContext(context.Context, h264.Packet) error
}

// EncodeStream同步连接帧源、Encoder与AU目标；在取下个输入前排空可用输出，
// 因而慢Writer直接背压，不启动goroutine、不维护额外输入队列。输入帧均Release。
// 正常源EOF自动Flush并排空B/Lookahead，最后返回io.EOF；其他错误不Flush半条流。
// 每次端点/codec操作前后检查ctx，取消优先返回ctx.Err，不关闭调用方I/O。
// 普通端点必须在一次帧操作内返回；阻塞网络/管道应实现Context*接口或由所有者
// 在取消时中断底层I/O。不能用不可中断的io.Reader宣称及时取消。
func EncodeStream(ctx context.Context, src FrameReader, dst AccessUnitWriter, cfg EncodeConfig) error {
	limit, err := pipelineLimit(ctx, src == nil || dst == nil, cfg.MaxInputBuffer)
	if err != nil {
		return err
	}
	if r, ok := src.(*YUVReader); ok && r.bytes > limit {
		return fmt.Errorf("stream: YUV输入超出MaxInputBuffer: %w", h264.ErrResourceLimit)
	}
	enc, err := encoder.New(cfg.Encoder, cfg.Options...)
	if err != nil {
		return err
	}
	defer enc.Close()
	drain := func(flushed bool) error {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			p, err := enc.ReceivePacket()
			if err != nil {
				if errors.Is(err, h264.ErrAgain) && !flushed {
					return nil
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if w, ok := dst.(ContextAccessUnitWriter); ok {
				err = w.WriteAccessUnitContext(ctx, p)
			} else {
				err = dst.WriteAccessUnit(p)
			}
			if e := ctx.Err(); e != nil {
				return e
			}
			if err != nil {
				return fmt.Errorf("stream: 写编码AU: %w", err)
			}
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var f *h264.Frame
		if r, ok := src.(ContextFrameReader); ok {
			f, err = r.ReadFrameContext(ctx)
		} else {
			f, err = src.ReadFrame()
		}
		if e := ctx.Err(); e != nil {
			if f != nil {
				f.Release()
			}
			return e
		}
		if err != nil {
			if f != nil {
				f.Release()
			}
			if !streamEOF(err) {
				return fmt.Errorf("stream: 读编码帧: %w", err)
			}
			if err := enc.Flush(); err != nil {
				return err
			}
			return drain(true)
		}
		if f == nil {
			return fmt.Errorf("stream: 帧源返回nil帧: %w", h264.ErrInvalidData)
		}
		err = checkInputFrame(f, limit)
		if err == nil {
			err = enc.SendFrame(f)
		}
		f.Release()
		if e := ctx.Err(); e != nil {
			return e
		}
		if err != nil {
			return fmt.Errorf("stream: 送编码帧: %w", err)
		}
		if err := drain(false); err != nil {
			return err
		}
	}
}

// DecodeStream同步消费完整Annex-B AU；自动Flush显示重排队列，所有输出帧无论
// 写出成功与否都Release。取消/背压/I/O所有权边界与EncodeStream相同。
// 对内置AUReader在组装复制前收紧AU预算；自定义源须自行限制分配，pipeline
// 在SendPacket前复核Data长度。源的单NAL预算仍须在NAL Reader构造时设置。
func DecodeStream(ctx context.Context, src AccessUnitReader, dst FrameWriter, cfg DecodeConfig) error {
	limit, err := pipelineLimit(ctx, src == nil || dst == nil, cfg.MaxInputBuffer)
	if err != nil {
		return err
	}
	dec, err := decoder.New(cfg.Decoder)
	if err != nil {
		return err
	}
	defer dec.Close()
	if r, ok := src.(*AUReader); ok {
		old := r.cfg.MaxAccessUnitSize
		r.cfg.MaxAccessUnitSize = min(old, limit)
		defer func() { r.cfg.MaxAccessUnitSize = old }()
	}
	drain := func(flushed bool) error {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			f, err := dec.ReceiveFrame()
			if err != nil {
				if errors.Is(err, h264.ErrAgain) && !flushed {
					return nil
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				f.Release()
				return err
			}
			if w, ok := dst.(ContextFrameWriter); ok {
				err = w.WriteFrameContext(ctx, f)
			} else {
				err = dst.WriteFrame(f)
			}
			f.Release()
			if e := ctx.Err(); e != nil {
				return e
			}
			if err != nil {
				return fmt.Errorf("stream: 写解码帧: %w", err)
			}
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var p h264.Packet
		if r, ok := src.(ContextAccessUnitReader); ok {
			p, err = r.ReadAccessUnitContext(ctx)
		} else {
			p, err = src.ReadAccessUnit()
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if err != nil {
			if !streamEOF(err) {
				return fmt.Errorf("stream: 读解码AU: %w", err)
			}
			if err := dec.Flush(); err != nil {
				return err
			}
			return drain(true)
		}
		if len(p.Data) > limit {
			return fmt.Errorf("stream: AU超出MaxInputBuffer: %w", h264.ErrResourceLimit)
		}
		if len(p.Data) == 0 {
			return fmt.Errorf("stream: AU源返回空Packet: %w", h264.ErrInvalidData)
		}
		if err := dec.SendPacket(p); err != nil {
			return fmt.Errorf("stream: 送解码AU: %w", err)
		}
		if err := drain(false); err != nil {
			return err
		}
	}
}

func pipelineLimit(ctx context.Context, missing bool, limit int) (int, error) {
	if ctx == nil || missing || limit < 0 {
		return 0, fmt.Errorf("stream: pipeline配置无效: %w", h264.ErrInvalidData)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if limit == 0 {
		limit = DefaultMaxInputBuffer
	}
	return limit, nil
}

func checkInputFrame(f *h264.Frame, limit int) error {
	if err := f.Validate(); err != nil {
		return err
	}
	for _, p := range f.Planes {
		if len(p.Data) > limit {
			return fmt.Errorf("stream: 帧超出MaxInputBuffer: %w", h264.ErrResourceLimit)
		}
		limit -= len(p.Data)
	}
	return nil
}

// 损坏尾数据可能同时包装EOF和InvalidData，不能将其当作正常输入结束。
func streamEOF(err error) bool {
	return errors.Is(err, io.EOF) && !errors.Is(err, h264.ErrInvalidData) && !errors.Is(err, io.ErrUnexpectedEOF)
}
