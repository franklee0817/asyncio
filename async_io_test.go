package asyncio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var finErr = errors.New("test finished")

type testItem struct {
	id int64
}

func (i *testItem) GetCursor() int64 {
	return i.id
}

type testReader struct{}

func newReader() *testReader {
	return new(testReader)
}

func (r *testReader) Read(ctx context.Context, cursor int64, size uint32) ([]CursorGetter, error) {
	if cursor > 100000 {
		return nil, finErr
	}
	var items []CursorGetter
	for i := cursor; i < cursor+int64(size); i++ {
		items = append(items, &testItem{id: i + 1})
	}

	return items, nil
}

type testWriter struct {
	name string
	t    *testing.T
}

func newWriter(t *testing.T, n string) *testWriter {
	w := new(testWriter)
	w.name = n
	w.t = t
	return w
}

func (w *testWriter) Name() string {
	return w.name
}

func (w *testWriter) Write(ctx context.Context, bucket []CursorGetter, cursor int64) error {
	var items []*testItem
	for idx := range bucket {
		items = append(items, bucket[idx].(*testItem))
	}
	sort.Slice(items, func(x, y int) bool {
		return items[x].GetCursor() > items[y].GetCursor()
	})

	for idx := range items {
		item := items[idx]
		assert.EqualValues(w.t, item.GetCursor(), cursor-int64(idx))
	}

	return nil
}

func TestOneToOneAsync(t *testing.T) {
	r := newReader()
	w := newWriter(t, "A")
	ctrl, err := New(WithReader(r), WithWriter(w))
	assert.Nil(t, err)

	err = ctrl.Start(context.Background(), 0)
	assert.EqualError(t, err, "数据读取失败,err:test finished")
}

func TestOneToThreeAsync(t *testing.T) {
	r := newReader()
	w1 := newWriter(t, "A")
	w2 := newWriter(t, "B")
	w3 := newWriter(t, "C")
	ctrl, err := New(WithReader(r), WithWriter(w1))
	assert.Nil(t, err)
	ctrl.wbs = 500
	child1, err := New(EnableCtrlReader(), WithWriter(w2))
	assert.Nil(t, err)
	child1.wbs = 200
	child2, err := New(EnableCtrlReader(), WithWriter(w3))
	assert.Nil(t, err)
	child2.wbs = 1000
	err = ctrl.TransferTo(child1)
	assert.Nil(t, err)
	err = ctrl.TransferTo(child2)
	assert.Nil(t, err)

	err = ctrl.Start(context.Background(), 0)
	assert.EqualError(t, err, "数据读取失败,err:test finished")
}

func BenchmarkController_Start(b *testing.B) {
	t := new(testing.T)
	r := newReader()
	w := newWriter(t, "A")
	ctrl, err := New(WithReader(r), WithWriter(w))
	assert.Nil(t, err)

	err = ctrl.Start(context.Background(), 0)
	assert.EqualError(t, err, "数据读取失败,err:test finished")
}

func CalAggTime15(ts int64) int64 {
	// 1. 秒时间戳转为分钟
	mts := int64(ts/60) * 60

	// 2. 以15分钟为一个格子计算当前时间应当落在当日的哪一个格子中
	t := time.Unix(ts, 0)
	mt := int64(math.Ceil(float64(t.Minute())/15.0)) * 15
	mts += (mt - int64(t.Minute())) * 60

	return mts
}

func TestAny(t *testing.T) {
	layout := "2006-01-02 15:04:05"
	now := time.Now()
	fmt.Println(now.Format(layout))

	target := CalAggTime15(now.Unix())
	fmt.Println(time.Unix(target, 0).Format(layout))
}
