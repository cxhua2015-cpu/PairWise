# readyqueue325

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引是以任务 ID 为键的哈希表（`map[string]Item`），入队、取消、按 ID 去重均为 O(1) 均摊。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序结构，而是在 `Pop`/`Snapshot` 时对候选集按需排序，以换取写入路径的简洁与回滚的低成本。

### 候选事务
- `Apply` 先在持锁状态下对整个批次做**完整结构校验**（kind、ID 字符集与字节上限、非负时间），再读取任何状态。
- 通过校验后，把当前索引克隆为一份**候选副本**，在副本上顺序执行 Enqueue/Cancel；revision 在候选上递增。
- 任一步失败（`ErrExists`/`ErrNotFound`）或**末尾容量检查**失败（`ErrCapacity`）时直接丢弃候选：时间、状态与 revision 计数器自然回滚，无需补偿日志。
- 全部成功才一次性提交：交换索引、推进单调时间、generation 恰好加一、提交 revision 计数器。空批次是完全无操作，generation 不变。

### 所有权
- 所有公开方法在单把互斥锁下执行，支持任意并发调用；`Pop` 的选中与删除在同一临界区内原子完成。
- `Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改返回值不会影响队列内部状态。

### 复杂度
- `Apply`：O(n + k)，n 为当前任务数（克隆索引），k 为批内操作数。
- `Pop`：O(n + r log r)，r 为就绪任务数（r ≤ n）。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
