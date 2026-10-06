# readyqueue220

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义见 `SPEC.md`。

## 用法

```go
q, _ := readyqueue220.New(readyqueue220.Options{MaxItems: 4, MaxIDBytes: 8})
r, _ := q.Apply(readyqueue220.Batch{Now: 1, Ops: []readyqueue220.Op{
    {Kind: readyqueue220.Enqueue, ID: "a", Priority: 2, ReadyAt: 0},
}})
items, _ := q.Pop(1, 1)
snap := q.Snapshot()
```

运行演示：`go run ./cmd/demo`。

## 设计说明

**索引**：内部使用 `map[string]Item` 以 ID 为键做 O(1) 查找/删除；规范顺序
（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时按需排序，
不为有序性维护额外堆结构——容量受 `MaxItems` 约束，排序开销有界。

**候选事务**：`Apply` 先对整个批次做完整结构校验（Now 非负、kind 合法、
ID 字符集与字节上限、ReadyAt 非负），再检查时间单调性，然后在状态克隆上
顺序执行 Enqueue/Cancel 并分配 revision，最后才检查容量。任一步失败
（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选状态，时间、
generation、revision 计数与条目集合全部保持不变，实现原子回滚。
非空成功批次 generation 恰好加一；空批次为无操作。

**所有权**：队列独占内部 map；`Pop` 弹出的条目已从队列原子删除，
`Snapshot`/`Pop` 返回的切片均为新分配的副本，调用方修改不影响内部状态。
所有公开方法由同一把互斥锁保护，可安全并发调用。

**复杂度**：`Apply` 为 O(k·n)（k 为批次大小，克隆 n 个条目，可优化为
增量 undo 日志）；`Pop` 与 `Snapshot` 为 O(n log n)（排序）；`New` O(1)。
空间 O(n)。
