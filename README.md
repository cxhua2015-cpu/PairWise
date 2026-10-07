# readyqueue340

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 用法

```go
q, _ := readyqueue340.New(readyqueue340.Options{MaxItems: 1024, MaxIDBytes: 64})
res, _ := q.Apply(readyqueue340.Batch{Now: 1, Ops: []readyqueue340.Op{
    {Kind: readyqueue340.Enqueue, ID: "job-a", Priority: 5, ReadyAt: 2},
}})
items, _ := q.Pop(2, 10) // ReadyAt<=2，按 Priority 降序、ReadyAt 升序、ID 升序
snap := q.Snapshot()
```

## 设计说明

**索引**：内部使用 `map[string]Item` 作为唯一索引，按 ID 提供 O(1) 的存在性判断、入队与取消。未维护额外的堆结构——容量受 `MaxItems` 约束，Pop/Snapshot 时一次性收集候选并排序，换取实现的简单与正确性。

**候选事务**：`Apply` 先做整批结构校验（kind、ID 字符集与字节上限、非负时间），再做单调时间检查；随后在克隆的 map 与 revision 计数器上顺序执行 Enqueue/Cancel，最后才检查最终容量。任一步失败直接丢弃候选状态，时间、条目与 revision 天然回滚；成功时整体换入，非空批次 generation 恰好加一。

**所有权**：所有公开方法共享一把 `sync.Mutex`，可任意并发调用。`Pop` 在锁内原子地选择并删除就绪条目；`Snapshot` 返回按规范顺序排序的新切片，调用方修改返回值不影响内部状态。

**复杂度**（n 为当前条目数，b 为批大小，k 为就绪数）：
- `Apply`：O(n + b)（克隆 + 顺序应用），容量检查 O(1)。
- `Pop`：O(n + k log k)（筛选 + 排序 + 删除）。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
