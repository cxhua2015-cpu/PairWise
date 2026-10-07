# readyqueue345

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。

## 语义概要

- 显式非负单调时间：`Apply`/`Pop` 携带 `now`，倒退返回 `ErrTime`，负值返回 `ErrInvalidInput`。
- `Apply` 原子顺序执行 `Enqueue`/`Cancel`：`Enqueue` 分配单调递增的 revision，`Cancel` 移除任务；容量上限只在批次末尾检查（允许先 Cancel 再 Enqueue 的置换批次）。
- 失败整体回滚：时间、条目状态、revision 计数与 generation 全部恢复原状。
- 非空成功批次 generation 只增加一次；空批次不改变任何状态。
- `Pop(now, n)` 选取 `ReadyAt <= now` 的任务，按 Priority 降序、ReadyAt 升序、ID 升序排序，原子删除后返回。
- ID 仅允许非空 ASCII 小写字母、数字、`-`、`_`，且不超过 `Options.MaxIDBytes`；`Options` 中容量与长度上限必须为正。

## 实现说明

- **索引**：内部使用 `map[string]Item` 按 ID 索引，`Enqueue`/`Cancel` 与存在性检查均为 O(1)。
- **候选事务**：`Apply` 先做整批结构校验（kind、ID 字符集与长度、非负时间），不触碰状态；随后在单把互斥锁内顺序执行，并记录逆操作日志（undo log）。任一步失败或末尾容量超限时，按逆序回放日志恢复条目，并恢复 revision 计数与队列时间，实现事务式回滚。
- **所有权**：所有公开方法通过一把 `sync.Mutex` 串行化，天然并发安全；`Pop` 与 `Snapshot` 返回的切片均为新分配的副本，调用方修改不会影响内部状态。
- **复杂度**：`Apply` 为 O(k)，k 为批次内操作数；`Pop` 与 `Snapshot` 为 O(n log n)（收集就绪候选后排序），其中 n 为队列内条目数；空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
