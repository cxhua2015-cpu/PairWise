# readyqueue435

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构与多文件联动

- `prioritybox.go` — 核心事务引擎：`Queue`、类型与错误值、`New`/`Apply`/`Pop`/`Snapshot`。
- `validation.go` — 无副作用的批次结构预检：`ValidateBatch` 与 `Apply` 共享同一 `validateBatch`，只校验结构（时间非负、kind 合法、ID 字符集与字节上限、Cancel 不得携带 Priority/ReadyAt、Enqueue 的 ReadyAt 非负），不读取队列状态。
- `stats.go` — `Stats` 在同一把互斥锁内读取，提供线性一致的 Generation/NextRevision/Now/Items 摘要。
- `clone.go` — `Clone` 在锁内深拷贝全部字段（含逻辑时钟 now、generation、nextRevision），返回完全独立所有权的队列。
- `preview.go` — `Preview` 先做结构预检，再在接收者的一次线性化快照上 `Clone` 出候选队列，复用完整 `Apply` 事务语义，返回候选 `Result`、`Snapshot`、`Stats`；原对象的状态、generation、revision 与逻辑时钟均不变，失败时全部返回零值且错误与 `Apply` 一致。

## 索引与事务

- **索引**：主索引为 `map[string]Item`（按 ID 去重，Enqueue/取消 O(1)）；规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时按需排序物化，不维护额外有序结构。
- **候选事务**：`Apply` 先完整结构校验，再在锁内把批次顺序应用到一份候选 map（copy-on-write）上；Enqueue 分配单调递增 revision，容量只在批次末尾检查。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`/`ErrTime`）直接丢弃候选，时间、状态与 revision 计数全部回滚；非空成功批次 generation 恰好加一，空批次不改变任何状态。
- **所有权**：`Pop`、`Snapshot`、`Clone`、`Preview` 返回的切片与 map 均为新建副本，调用方修改不会影响队列内部状态；`Clone` 与 `Preview` 的候选队列与原体完全隔离。
- **并发**：所有公开方法共用一把 `sync.Mutex`，每个方法整体线性化，可安全并发调用。

## 复杂度

- `Apply`：O(k·n) 拷贝候选 + O(k) 应用（k 为批次大小，n 为当前元素数）。
- `Pop`：O(n log n) 排序就绪集合，删除 O(limit)。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)；`ValidateBatch`：O(k·L)（L 为 ID 长度）。
- `Preview`：一次 `Clone` + 一次候选 `Apply` + `Snapshot`/`Stats`。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
