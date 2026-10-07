# readyqueue385

并发安全的内存型“就绪优先队列”，实现见 `readyqueue385/prioritybox.go`，规范见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

**索引**：队列主体为 `map[string]Item`，以 ID 为键，Enqueue/Cancel/存在性判断均为 O(1)。Pop 与 Snapshot 采用全量扫描加排序（Priority 降序、ReadyAt 升序、ID 升序），规模受 `MaxItems` 上限约束，无需额外堆结构。

**候选事务**：`Apply` 先在持锁状态下对整个批次做纯结构校验（时间非负、Kind 合法、ID 字符集与字节上限），再检查时间单调性；随后在克隆的候选 map 上顺序执行 Enqueue/Cancel 并推进候选 revision 计数器，容量只在末尾检查。任一步失败直接返回，队列的时间、状态、generation 与 revision 全部保持原值；全部成功才一次性提交，非空批次 generation 恰好加一。

**所有权**：所有公开方法共用一把 `sync.Mutex`，可任意并发调用。`Snapshot` 返回的切片与 `Pop` 返回的 `[]Item` 均为新建副本，调用方修改不影响内部状态；`Item` 为纯值类型，无共享指针。

**复杂度**：Enqueue/Cancel 单 op 均摊 O(1)，批次为 O(k·n)（k 为 op 数，n 为当前元素数，源于候选克隆）；Pop 与 Snapshot 为 O(n log n)；空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
