package asyncio

import "context"

// CursorGetter cursor getter的接口抽象
// 被加工的数据的结构需要实现这个接口，以便在加工的过程中可以顺利找到当前的offset
type CursorGetter interface {
	// GetCursor 获取当前数据所属的offset（通常为自增ID）
	GetCursor() int64
}

// Reader 读取数据的reader抽象，作为异步I/O的数据加载器
type Reader interface {
	// Read 读取接口，用于从指定内容源读取以offset为起点的size条数据
	// []CursorGetter不建议使用值类型的slice，使用值类型的slice在链式拼接的过程中会出现频繁的值拷贝
	// 读取到数据slice长度为0时程序正常退出
	// 读取到error时异常停止
	Read(ctx context.Context, cursor int64, size uint32) (bucket []CursorGetter, err error)
}

// Writer 写数据的writer抽象，作为异步I/O的数据输出方
type Writer interface {
	// Write 写数据的方法，可以是写到存储中，也可以是写到内存中
	// 如果需要一路读取，多路写入，可以使用TransferTo进行转接，具体使用方法详见ReadMe
	Write(ctx context.Context, bucket []CursorGetter, cursor int64) error
	// Name 用于多writer场景下输出错误或错误信息时标注writer身份
	Name() string
}

// BucketLimiter 读写的内容块大小控制器，用于动态控制读和写的速率
type BucketLimiter interface {
	// GetReaderSize 获取reader单次读取的数据行数，如MySQL的limit参数，默认50
	GetReaderSize() uint32
	// GetWriterSize 获取writer单次写入的数据行数，批量插入控制，默认100
	GetWriterSize() uint32
}
