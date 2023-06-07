package asyncio

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"
)

// IOMonitor 读写监控器
type IOMonitor struct {
	// 标记每一轮开始读数据的时间，用于计算读取耗时
	rt time.Time
	// 读取总耗时
	rc time.Duration
	// 读取次数
	rtc uint32
	// 读取数据条数
	rn uint32

	// 标记每一轮开始写数据的时间，用于计算写入耗时
	wt time.Time
	// 写入总耗时
	wc time.Duration
	// 写入次数
	wtc uint32
	// 写入数据条数
	wn uint32

	// 上报的ticker
	t *time.Ticker

	// monitor引用，当引用存在时，表示读计数由引用提供
	refer *IOMonitor

	name string
}

// NewMonitor 构造函数
func NewMonitor(name string, reportDur time.Duration) *IOMonitor {
	m := new(IOMonitor)
	m.t = time.NewTicker(reportDur)
	m.name = name

	return m
}

// Report 汇报读写效率
func (m *IOMonitor) Report() {
	rm := m.findParent()
	if rm.rc == 0 || rm.rn == 0 || rm.rtc == 0 {
		return
	}
	if m.wc == 0 || m.wn == 0 || m.wtc == 0 {
		return
	}
	readNAvg := rm.rc / time.Duration(rm.rn)
	writeNAvg := m.wc / time.Duration(m.wn)
	rate := math.Floor(float64(readNAvg)/float64(writeNAvg)*100) / 100

	readCAvg := rm.rc / time.Duration(rm.rtc)
	writeCAvg := m.wc / time.Duration(m.wtc)

	report := fmt.Sprintf("读取次数: %v次\n读取数据: %v条\n读取耗时: %v\n平均读耗时: %s/次\n平均读耗时: %s/条\n"+
		"写入次数: %v次\n写入数据: %v条\n写入耗时: %v\n平均写耗时: %s/次\n平均写耗时: %s/条\nI/O耗时比: %v",
		rm.rtc, rm.rn, rm.rc, readCAvg, readNAvg,
		m.wtc, m.wn, m.wc, writeCAvg, writeNAvg, rate)
	if rate > 1 {
		report += "\n读耗时大于写，建议增大读取数据的bucket大小，或者优化Reader代码"
	} else if rate < 1 {
		report += "\n写耗时大于读，建议增大写入数据的bucket大小，或者优化Writer代码"
	}
	log.Printf("===========================%s读写效率报告==============================\n%s", m.name, report)
}

func (m *IOMonitor) findParent() *IOMonitor {
	if m.refer != nil {
		return m.refer.findParent()
	}

	return m
}

// StartRead 开始读打点
func (m *IOMonitor) StartRead() {
	// 每50次清零一次
	if m.rtc == 50 {
		m.rtc = 0
		m.rc = 0
		m.rn = 0
	}
	m.rt = time.Now()
}

// FinishRead 完成读取打点
func (m *IOMonitor) FinishRead(bucketSize uint32) {
	m.rn += bucketSize
	m.rtc++
	m.rc += time.Since(m.rt)
}

// StartWrite 开始写入 打点
func (m *IOMonitor) StartWrite() {
	// 每50次清零一次
	if m.wtc == 50 {
		m.wtc = 0
		m.wc = 0
		m.wn = 0
	}
	m.wt = time.Now()
}

// FinishWrite 完成写入 打点
func (m *IOMonitor) FinishWrite(bucketSize uint32) {
	m.wn += bucketSize
	m.wtc++
	m.wc += time.Since(m.wt)
}

// Start 开始打点
func (m *IOMonitor) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.t.C:
			m.Report()
		}
	}
}
