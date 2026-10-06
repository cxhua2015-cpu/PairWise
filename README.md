# readyqueue300

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 架构（多文件联动）

- `prioritybox.go` — 核心事务引擎：`New`/`Apply`/`Pop`/`Snapshot`、排序比较器与内部状态。
- `validation.go` — 无副作用的批次结构预检：`ValidateBatch` 与 `Apply` 共享同一个
  `validateStructural`，保证“先完整结构校验、再读取状态”的语义完全一致。
- `stats.go` — `Stats`：在队列互斥锁内读取的线性一致状态摘要。
- `clone.go` — `Clone`：复制逻辑时钟（now/generation/nextRevision）与全部条目的深拷贝。

## 索引

主索引是 `map[string]Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）。
弹出序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时对候选切片即时排序，
不为排序序维护额外索引，从而使事务路径保持 O(1) 摊销。

## 候选事务

`Apply` 在单个互斥锁内串行执行整批操作：先做完整结构校验（不读状态），再校验单调时间，
然后顺序执行 Enqueue/Cancel，容量只在批次末尾检查一次。执行期间记录逆操作日志
（Enqueue 的逆操作是删除，Cancel 的逆操作是恢复原条目）；任一步失败即逆序回放日志，
并恢复 `now` 与 `nextRevision`，实现时间、状态与 revision 的完整回滚。非空成功批次
`generation` 恰好加一，空批次不改变任何状态。

## 所有权

所有公开方法返回的数据（`Pop` 的切片、`Snapshot`、`Stats`）都是在锁内新分配的副本，
与内部状态完全隔离；`Clone` 复制 map 与逻辑时钟，克隆体与原队列互不影响。
字符串 ID 不可变，无需额外拷贝。

## 复杂度

- `Apply`（k 个操作）：O(k) 均摊，回滚同为 O(k)。
- `Pop`（n 个条目，r 个就绪）：O(n + r log r)。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。
- 空间：O(n)。

## 并发

所有公开方法（`Apply`/`Pop`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）均可并发调用；
可变状态由单一 `sync.Mutex` 保护，`ValidateBatch` 只读取不可变配置，无数据竞争。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
