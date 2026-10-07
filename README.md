# readyqueue360

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

**索引与所有权**
- 队列内部以 `map[string]Item` 作为唯一所有者索引：ID → 条目，O(1) 判重（`ErrExists`）与取消（`ErrNotFound`）。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不持久化维护，而是在 `Pop`/`Snapshot` 时对候选集即时排序，避免堆结构在部分回滚下的复杂性。
- 所有公开方法由单个 `sync.Mutex` 保护，返回的切片均为新分配的副本，与内部状态完全隔离。

**候选事务（Apply）**
1. 先做整批结构校验（Now 非负、Kind 合法、ID 字符集与字节上限），再读取任何状态。
2. 在锁内检查时间单调性（`Now < now` → `ErrTime`）。
3. 将当前 map 复制为候选副本，在副本上顺序执行 Enqueue/Cancel；Enqueue 从单调递增的 `nextRevision` 分配 revision。
4. 最终容量只在末尾检查一次（`len > MaxItems` → `ErrCapacity`），因此同批“先 Cancel 再 Enqueue”可以合法通过。
5. 任一步失败直接返回：候选副本与递增的 revision 计数被丢弃，时间、状态、revision 天然回滚，无需补偿日志。
6. 成功时一次性提交：替换 map、推进 `now` 与 `nextRevision`；非空批次 generation 恰好 +1，空批次不变。

**复杂度**（n = 队列中条目数，b = 批次大小，k = Pop 的 limit）
- `Apply`：O(n + b)（复制候选 map + 顺序执行），无额外堆维护。
- `Pop`：O(n log n)（筛选 ReadyAt ≤ now 后排序），删除 O(k)。
- `Snapshot`：O(n log n)（复制并排序）。
- 空间：O(n)。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
