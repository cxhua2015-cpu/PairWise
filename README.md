# prioritybox

并发安全的内存型“优先级信箱”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序结构；Pop/Snapshot 时对候选集即时排序，实现简单且在信箱规模下足够快。

**候选事务**
- `Apply` 先对整个批次做完整结构校验（时间非负、kind 合法、ID 字符集与长度），不读取任何状态。
- 通过校验后在互斥锁内把当前 map 克隆为候选副本，按顺序在副本上执行 Enqueue/Cancel 并分配 revision；容量只在末尾对最终大小检查。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃副本：时间、状态、revision 计数器全部保持原值，实现原子回滚；成功才一次性提交并令 generation 恰好 +1。空批次不改变任何状态。

**所有权**
- 队列内部状态（map、时间、计数器）完全由 `Queue` 持有，仅在被 `sync.Mutex` 保护的方法内访问，所有公开方法可并发调用。
- `Pop`/`Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态；`Item` 为纯值类型，无共享指针。

**复杂度**（n = 队列大小，b = 批次大小，k = 就绪任务数）
- `Apply`：O(n + b)，克隆加顺序执行。
- `Pop`：O(n + k log k)，筛选就绪集合并排序，删除为 O(k)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
