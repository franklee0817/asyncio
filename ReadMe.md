### 异步I/O控制器

#### 一、 概述

这是一个简单的通过抽象读写流程，实现读写分离，从而最大化数据读写的工具。使用之前请先关注 `interface.go`，其中定义了业务应用需要实现的所有接口。

#### 二、名词解释

| 名词            | 释义                                                                                |
|---------------|-----------------------------------------------------------------------------------|
| cursor        | 数据读取时用于排序的key的值，如: select * from tbl_a where id > $cursor order by id asc.        |
| Reader        | 数据读取结构的抽象，用户需要自行实现其定义的Read方法，读取数据时需要保证数据按照cursor对应的key顺序排列                        |
| Writer        | 数据写入或消费的结构抽象，用户需要自行实现Writer的Write方法，并尽可能保证数据的写入和offset持久化幂等性。Name方法主要为日志提供支持，便于辨认 |
| CursorGetter  | 数据自身结构需要实现这个接口，程序在调度流程过程中获取到当前的cursor值后会传递给Reader读取下一批次数据，同时也会传递给writer进行持久化      |
| BucketLimiter | 数据读写速率控制器，可以自行实现并动态控制读取和写入的bucket大小，从而匹配读写资源的效率差                                  |


#### 三、 使用方式

1. 单一读写
```golang
package xxx

import (
	"context"
	"time"

	"github.com/franklee0817/asyncio"
)

type mLogic struct{}

func (l *mLogic) Read(ctx context.Context, cursor int64, size uint32) ([]asyncio.CursorGetter, error) {
	... reading data ...
	return data, err
}

func (l *mLogic) Write(ctx context.Context, bucket []asyncio.CursorGetter, cursor int64) error {
	... writing data ...

	return err
}

func (l *mLogic) Name() string {
	return "My Test Logic"
}

func ReadAndWrite() error {
	l := new(mLogic)
	io := asyncio.New(asyncio.WithReader(l),
		asyncio.WithWriter(l),
		asyncio.WithBufferSize(4096),
		asyncio.WithReaderWait(50*time.Millisecond),
		asyncio.WithWriterWait(100*time.Millisecond))
	return io.Start(context.Background(), 0)
}
```

2. 多路写
```golang
package xxx

import (
	"context"
	"fmt"
	"time"

	"github.com/franklee0817/asyncio"
)

type dbLogic struct{}

func (db *dbLogic) Read(ctx context.Context, cursor int64, size uint32) ([]asyncio.CursorGetter, error) {
	... reading data ...
	return data, err
}

func (db *dbLogic) Write(ctx context.Context, bucket []asyncio.CursorGetter, cursor int64) error {
	... writing data ...

	return err
}

func (db *dbLogic) Name() string {
	return "My DB Writer"
}

type esWriter struct {}

func (db *esWriter) Write(ctx context.Context, bucket []asyncio.CursorGetter, cursor int64) error {
	... writing data ...

	return err
}

func (db *esWriter) Name() string {
	return "My ES Writer"
}

func SingleReadMultiWrite() error {
	db := new(dbLogic)
	io := asyncio.New(asyncio.WithReader(db),
		asyncio.WithWriter(db),
		asyncio.WithBufferSize(4096),
		asyncio.WithReaderWait(50*time.Millisecond),
		asyncio.WithWriterWait(100*time.Millisecond))
	es := new(esWriter)
	esIO := asyncio.New(asyncio.WithWriter(es),
		asyncio.WithBufferSize(4096),
		asyncio.WithReaderWait(50*time.Millisecond),
		asyncio.WithWriterWait(100*time.Millisecond),
		asyncio.EnableCtrlReader())
	if err := io.TransferTo(esIO); err != nil {
		return fmt.Errorf("数据转接配置出错，err:%v", err)
    }
	return io.Start(context.Background(), 0)
}
```
