package asyncio

import "time"

// Option 配置函数类型什么
type Option func(ctrl *Controller)

// WithReader 配置reader
func WithReader(r Reader) Option {
	return func(ctrl *Controller) {
		ctrl.r = r
	}
}

// EnableCtrlReader 配置当前控制器为子控制器，数据来源来自于上层Controller
// Caution:
//		Controller本身只支持单路读写，如果需要一个读搭配多路写的话，可以创建新的Controller
//		然后通过这个EnableCtrlReader的方式实现新增一路写，父级Controller会自动将数据转接给新增的这一路
func EnableCtrlReader() Option {
	return func(ctrl *Controller) {
		ctrl.isChild = true
	}
}

// EnableIOReport 启用I/O耗时比监控
func EnableIOReport() Option {
	return func(ctrl *Controller) {
		ctrl.enableMonitor = true
	}
}

// WithWriter 配置writer
func WithWriter(w Writer) Option {
	return func(ctrl *Controller) {
		ctrl.w = w
	}
}

// WithBufferSize 配置buffer大小
func WithBufferSize(size uint32) Option {
	return func(ctrl *Controller) {
		ctrl.bs = size
	}
}

// WithBucketLimiter 配置reader和writer读写数据大小的动态控制器
func WithBucketLimiter(limiter BucketLimiter) Option {
	return func(ctrl *Controller) {
		ctrl.bl = limiter
	}
}

// WithReaderWait 配置读取数据的时间间隔，降低被读资源压力
func WithReaderWait(wait time.Duration) Option {
	return func(ctrl *Controller) {
		ctrl.rwd = wait
	}
}

// WithWriterWait 配置写入数据的时间间隔，降低被写资源的压力
func WithWriterWait(wait time.Duration) Option {
	return func(ctrl *Controller) {
		ctrl.wwd = wait
	}
}
