# taskqueue110

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，以任务 ID 为键，Enqueue/Cancel 的存在性判断与删除均为 O(1)。
- 不维护持久堆；`Pop`/`Snapshot` 时按需筛选并排序，以换取事务回滚的简单性与正确性。

### 候选事务
- `Apply` 先做完整结构校验（时间非负、kind 合法、ID 字符集与长度），不读取任何状态。
- 非空批次在候选副本（克隆的 map + 暂存 revision 计数器）上顺序执行 Enqueue/Cancel；任一操作失败即丢弃副本。
- 容量检查只在全部操作执行完后进行（`len(cand) > MaxItems` → `ErrCapacity`）。
- 全部成功才一次性提交：替换 map、推进 `nextRevision`、推进单调时间 `now`、`generation++`。失败时时间、状态、revision 完全不变。

### 所有权与并发
- 所有公开方法由一把 `sync.Mutex` 保护，可任意并发调用。
- 返回的切片（`Pop`、`Snapshot.Items`）均为新分配的副本，调用方修改不影响内部状态。
- 时间为显式非负单调值：`now` 小于当前时钟返回 `ErrTime`，且失败不推进时钟。

### 复杂度
- `New`：O(1)；`Apply`：O(n + m)，n 为当前任务数（克隆），m 为批内操作数。
- `Pop`：O(n + k log k)，k 为就绪任务数；`Snapshot`：O(n log n)。
- 空间：O(n)，n ≤ `MaxItems`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
