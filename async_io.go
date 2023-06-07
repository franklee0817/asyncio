/*Package asyncio
这是一个用来调度异步I/O实现读写分离的程序，外部程序在实现interface.go中的接口后即可轻松的构建一个读写分离的异步I/O程序
程序通过offset机制来保证数据读写的一致性，写入时offset的持久化交由用户处理，用户需要保证写入的幂等性。
当I/O效率不匹配时，可以通过定义BucketLimiter实现动态调整I/O的bucket size，从而实现动态控速
*/
package asyncio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	// DefaultReaderBucketSize 默认单批次读取数据条数
	DefaultReaderBucketSize = 50
	// DefaultWriterBucketSize 默认单批次写入数据条数
	DefaultWriterBucketSize = 100

	// DefaultBufferChanSize 默认读写buffer大小
	DefaultBufferChanSize = 1024

	// DefaultReaderWaitDur 每批次读取数据的休眠时间
	DefaultReaderWaitDur = 10 * time.Millisecond
	// DefaultWriterWaitDur 每批次写入数据的休眠时间
	DefaultWriterWaitDur = 10 * time.Millisecond
)

// ErrIgnoreEmptyBucket 忽略读到空bucket的问题，继续读取
var ErrIgnoreEmptyBucket = errors.New("read empty bucket,ignore")

// BufChan buffer channel 放入到缓冲队列中的数据需要是实现了OffsetGetter接口的对象
type BufChan chan CursorGetter

// Controller 异步I/O的控制器申明
type Controller struct {
	// 数据读取来源
	r Reader
	// 数据输出目标
	w Writer
	// 数据转接，通过 Transfer方法进行连接
	// 假设当前控制器为A，将B连接到A的transfer上之后，则A的writer会作为B的reader
	// 既A在独立执行I/O工作的同时，将读到的数据共享给B
	children []*Controller
	isChild  bool // 标注是否为下游控制器，既上面提到的controller B...

	// buffer channels 异步读写的管道，用于从read向write传输数据
	bc BufChan
	// buffer size，用来申明读写之间buffer的大小
	// 底层由channel实现，所以当写快于读时会自动阻塞
	// 当读快于写时，此参数控制程序最大可容忍的读取的超前量
	// 默认值1024，当此值被设置为0时，会自动蜕化为同步读写
	bs uint32

	// read bucket size
	// reader每次写入数据的bucket的大小，默认值50
	rbs uint32
	// write bucket size
	// writer每次写入数据的bucket的大小，默认值100
	wbs uint32
	// reader和writer的bucket大小控制器，可动态调整每个批次的读取和写入的数据量
	bl BucketLimiter

	// read wait duration 标注读取的时间间隔，避免读取过快导致资源被拖垮，默认10ms
	rwd time.Duration
	// write wait duration 标注写入的时间间隔，避免频繁写入对资源带来过大压力，默认10ms
	wwd time.Duration

	// 标记当前控制器已执行结束
	finished bool

	monitor       *IOMonitor
	enableMonitor bool
}

// New 构造函数
func New(opts ...Option) (*Controller, error) {
	ctrl := &Controller{
		bs:            DefaultBufferChanSize,
		wbs:           DefaultWriterBucketSize,
		rbs:           DefaultReaderBucketSize,
		enableMonitor: false,
	}
	for _, opt := range opts {
		opt(ctrl)
	}
	// 必须至少有一个reader和一个writer
	if ctrl.r == nil && !ctrl.isChild {
		return nil, errors.New("请使用WithReader(someReader)配置一个读取数据的reader")
	}
	if ctrl.w == nil {
		return nil, errors.New("请使用WithWriter(someWriter)配置一个writer")
	}
	ctrl.bc = make(BufChan, ctrl.bs)
	ctrl.monitor = NewMonitor(ctrl.w.Name(), 30*time.Second)
	ctrl.fixDefaultCfg()

	return ctrl, nil
}

// TransferTo 数据转接，接收一个异步I/O控制器，将自身的输出作为输入传递给此控制器
func (ctrl *Controller) TransferTo(c *Controller) error {
	if !c.isChild {
		return errors.New("数据转接只支持配置了EnableCtrlReader的控制器")
	}
	if c.bc == nil {
		return errors.New("转接目标未正确通过New方法初始化")
	}
	ctrl.children = append(ctrl.children, c)
	c.monitor.refer = ctrl.monitor

	return nil
}

// Start 启动异步I/O
func (ctrl *Controller) Start(ctx context.Context, cursor int64) error {
	ctrl.activateWriters(ctx)
	ctrl.startMonitor(ctx)
	for {
		// 读取数据并写入buffer
		select {
		case <-ctx.Done():
			log.Printf("controller context done")
			return ctx.Err()
		default:
			bucket, err := ctrl.readBucket(ctx, cursor, ctrl.getReaderBucketSize())
			if err == ErrIgnoreEmptyBucket {
				ctrl.readerWait()
				continue
			}
			if err != nil {
				ctrl.waitForExit(ctx)
				return fmt.Errorf("数据读取失败,err:%v", err)
			}
			// 读取结束，退出流程
			if len(bucket) == 0 {
				ctrl.waitForExit(ctx)
				return nil
			}
			ctrl.passToWriters(bucket)
			cursor = bucket[len(bucket)-1].GetCursor()
			ctrl.readerWait()
		}
	}
}

func (ctrl *Controller) waitForExit(ctx context.Context) {
	ctrl.markReadDone()
	for {
		if ctrl.isAllFinished() {
			log.Printf("异步读写完成，控制器退出")
			return
		}
		time.Sleep(1 * time.Second)
	}
}
func (ctrl *Controller) startMonitor(ctx context.Context) {
	if ctrl.enableMonitor {
		go ctrl.monitor.Start(ctx)
		for _, c := range ctrl.children {
			if c.enableMonitor {
				go c.monitor.Start(ctx)
			}
		}
	}
}

// readBucket 读取一个bucket
func (ctrl *Controller) readBucket(ctx context.Context, cursor int64, size uint32) ([]CursorGetter, error) {
	if ctrl.enableMonitor {
		ctrl.monitor.StartRead()
	}
	bucket, err := ctrl.r.Read(ctx, cursor, size)
	if ctrl.enableMonitor {
		ctrl.monitor.FinishRead(uint32(len(bucket)))
	}

	return bucket, err
}

// fixDefaultCfg 修正默认参数
func (ctrl *Controller) fixDefaultCfg() {
	if ctrl.rbs == 0 {
		ctrl.rbs = DefaultReaderBucketSize
	}
	if ctrl.wbs == 0 {
		ctrl.wbs = DefaultWriterBucketSize
	}
	if ctrl.rwd == 0 {
		ctrl.rwd = DefaultReaderWaitDur
	}
	if ctrl.wwd == 0 {
		ctrl.wwd = DefaultWriterWaitDur
	}
}

// markReadDone reader读取数据完成后，关闭异步管道，通知下游执行结束
func (ctrl *Controller) markReadDone() {
	close(ctrl.bc)
	for _, child := range ctrl.children {
		close(child.bc)
	}
}

func (ctrl *Controller) isAllFinished() bool {
	finished := ctrl.finished
	for _, child := range ctrl.children {
		finished = finished && child.finished
	}

	return finished
}

// passToWriters 将数据传递给所有writer，包括children's writer
func (ctrl *Controller) passToWriters(bucket []CursorGetter) {
	wg := new(sync.WaitGroup)
	wg.Add(len(ctrl.children) + 1)
	go func(bc BufChan, rows []CursorGetter, grp *sync.WaitGroup) {
		defer grp.Done()
		for idx := range rows {
			bc <- rows[idx]
		}
	}(ctrl.bc, bucket, wg)

	for i := range ctrl.children {
		go func(bc BufChan, rows []CursorGetter, grp *sync.WaitGroup) {
			defer grp.Done()
			for idx := range rows {
				bc <- rows[idx]
			}
		}(ctrl.children[i].bc, bucket, wg)
	}
	wg.Wait()
}

// activateWriters 激活数据写协程
func (ctrl *Controller) activateWriters(ctx context.Context) {
	go ctrl.watchBuffer(ctx)
	for idx := range ctrl.children {
		go ctrl.children[idx].watchBuffer(ctx)
	}
}

// watchBuffer 分批消费buffer channel中的数据，每批数据大小保持在ctrl.wbs(write buffer size)指定的大小
// Note:
// 		1. wb既write buffer，只创建一次，通过控制index实现类似循环链表
// 		2. wb每写满一次则消费一次，消费结束后从头覆盖wb中的所有内容
func (ctrl *Controller) watchBuffer(ctx context.Context) {
	idx := 0
	size := int(ctrl.getWriterBucketSize())
	wb := make([]CursorGetter, size)
	for {
		// 开始写入
		select {
		case <-ctx.Done():
			return
		default:
			for item := range ctrl.bc {
				wb[idx%size] = item
				idx++
				if idx%size == 0 {
					ctrl.doWrite(ctx, wb)

					// 动态写入速率控制
					size = int(ctrl.getWriterBucketSize())
					if len(wb) != size {
						wb = make([]CursorGetter, size)
					}
				}
			}
			// 最后一个不完整的buffer的写入
			if idx%size > 0 {
				tmp := wb[:idx%size]
				ctrl.doWrite(ctx, tmp)
			}

			// 管道已关闭，程序执行完成
			ctrl.finished = true
		}
	}
}

// doWrite 执行数据写入，写入时出现错误则休眠ctrl.wd时间后继续尝试写入，直到写入成功为止
// Caution: 写入数据和写入cursor需要用户自行保证幂等
func (ctrl *Controller) doWrite(ctx context.Context, buffer []CursorGetter) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			cursor := buffer[len(buffer)-1].GetCursor()
			err := ctrl.writeBucket(context.Background(), buffer, cursor)
			ctrl.writerWait()
			if err == nil {
				return
			}
			log.Printf(fmt.Sprintf("writer【%s】数据写入失败(%s后重试)，err:%v", ctrl.w.Name(), ctrl.wwd, err))
		}
	}
}

func (ctrl *Controller) writeBucket(ctx context.Context, bucket []CursorGetter, cursor int64) error {
	if ctrl.enableMonitor {
		ctrl.monitor.StartWrite()
	}
	err := ctrl.w.Write(ctx, bucket, cursor)
	if ctrl.enableMonitor {
		ctrl.monitor.FinishWrite(uint32(len(bucket)))
	}

	return err
}

func (ctrl *Controller) getWriterBucketSize() uint32 {
	if ctrl.bl != nil && ctrl.bl.GetWriterSize() > 0 {
		return ctrl.bl.GetWriterSize()
	}

	return ctrl.wbs
}

func (ctrl *Controller) getReaderBucketSize() uint32 {
	if ctrl.bl != nil && ctrl.bl.GetReaderSize() > 0 {
		return ctrl.bl.GetReaderSize()
	}

	return ctrl.rbs
}

func (ctrl *Controller) writerWait() {
	if ctrl.wwd > 0 {
		time.Sleep(ctrl.wwd)
	}
}

func (ctrl *Controller) readerWait() {
	if ctrl.rwd > 0 {
		time.Sleep(ctrl.rwd)
	}
}
