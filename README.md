# readyqueue345

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**：队列内部以 `map[string]Item` 作为主索引，按 ID O(1) 定位任务，用于
Enqueue 的重复检测与 Cancel 的删除。弹出顺序（Priority 降序、ReadyAt 升序、ID 升序）
不维护额外有序结构，而是在 `Pop`/`Snapshot` 时对候选集排序，保证排序键定义唯一、无漂移。

**候选事务**：`Apply` 先对整个批次做纯结构校验（时间非负、kind 合法、ID 字符集与长度），
不触碰任何状态；随后在互斥锁内以“候选事务”方式顺序执行 Enqueue/Cancel，并记录撤销日志
（undo log）。任一操作失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）
时，按逆序回放撤销日志，完整回滚任务集合、时间与 revision 计数器，对外表现为全有或全无。
容量只在批次末尾检查，因此同批次内“先 Cancel 再 Enqueue”可以合法通过。

**所有权**：所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，可并发调用。
`Pop` 在选择与删除之间不释放锁，保证原子性。`Snapshot` 与 `Pop` 返回的切片均为新分配的
副本，调用方修改返回值不会影响队列内部状态。

**复杂度**（n 为队列中任务数，b 为批次数，k 为就绪候选数）：
- `New`：O(1)。
- `Apply`：结构校验 O(b·L)（L 为 ID 长度），执行 O(b)，末尾容量检查 O(1)；回滚 O(b)。
- `Pop`：扫描 O(n) + 候选排序 O(k log k) + 删除 O(k)。
- `Snapshot`：O(n log n)，返回隔离副本。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
