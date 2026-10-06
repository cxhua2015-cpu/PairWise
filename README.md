# readyqueue280

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## Multi-file architecture

实现按职责拆分为四个联动文件，共享同一套语义：

- `prioritybox.go` — 核心事务引擎：`New`/`Apply`/`Pop`/`Snapshot`，持有一把 `sync.Mutex` 保护全部内部状态（`now`、`generation`、`nextRevision`、`items`）。
- `validation.go` — 无副作用批次预检：`validateStructural` 是 `Apply` 与 `ValidateBatch` 共享的唯一结构校验入口，只读取不可变的配置上限，不触碰队列状态。
- `stats.go` — 线性一致统计：`Stats` 在同一互斥锁快照 `generation`/`nextRevision`/`now`/条目数，与并发事务状态严格一致。
- `clone.go` — 所有权安全的深拷贝：`Clone` 在锁内复制逻辑时钟与全部条目到全新 map，克隆体与原队列互不别名。

## 索引

主索引为 `map[string]Item`（按 ID 精确查找，Enqueue/Cancel 为 O(1)）。规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时对候选切片即时排序，不维护冗余有序结构，从而保证事务回滚零成本。

## 候选事务

`Apply` 先做完整结构校验，再在锁内把 `items` 复制为候选 map，顺序执行 Enqueue（分配单调递增 revision）/Cancel；容量只在末尾检查一次。任何失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`/`ErrTime`）直接丢弃候选，时间、状态与 revision 计数器天然回滚；成功才整体提交并使 `generation` 恰好加一（空批次不变）。

## 所有权

所有公开方法并发安全。`Snapshot`/`Pop` 返回的切片均为新建副本，与内部状态完全隔离；`Clone` 复制逻辑时钟且与原件零共享，任一队列的后续变更互不可见。

## 复杂度

- `Apply`：O(k + n)，k 为批次操作数、n 为当前条目数（候选复制）。
- `Pop`：O(n log n)（筛选就绪项并排序），删除 O(n)。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)；`ValidateBatch`：O(k)，无状态访问。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
