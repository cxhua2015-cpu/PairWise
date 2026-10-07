# readyqueue395

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引与数据结构

- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重（`ErrExists`）与 Cancel 查找（`ErrNotFound`）。
- 不维护持久堆；Pop/Snapshot 时按需收集候选并排序，规范顺序为 Priority 降序、ReadyAt 升序、ID 升序。该设计以单次全局互斥锁换取实现简单与强一致性，适合控制面中等规模场景。

## 候选事务（Apply）

- 批次先做完整结构校验（kind、ID 字符集与字节上限、非负 ReadyAt/Now），再读取任何状态；结构错误优先返回 `ErrInvalidInput`。
- 通过校验后在锁内把 `items` 复制到候选 map，顺序执行 Enqueue/Cancel：Enqueue 递增并分配 revision，Cancel 删除条目。
- 容量 `MaxItems` 只在末尾检查；任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`/`ErrTime`）直接丢弃候选 map，时间、状态与 revision 全部回滚，generation 不变。
- 成功时原子提交候选 map；非空批次 generation 恰好 +1，空批次不变，时间推进到 `Batch.Now`。

## 所有权与并发

- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可安全并发调用。
- `Pop` 返回的切片与 `Snapshot.Items` 均为新分配的副本，调用方修改不影响队列内部状态。
- 时间为显式非负单调值：成功调用推进 `now`，失败调用不推进。

## 复杂度

- `Apply`：O(n + k)，n 为当前条目数（候选复制），k 为批次操作数。
- `Pop`：O(n + r log r)，r 为就绪条目数。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
