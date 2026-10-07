# readyqueue440

并发安全的内存型“就绪优先队列”，Go 1.22+，仅标准库。语义见 `SPEC.md`。

## 架构与索引

- `prioritybox.go`：核心事务引擎。`Queue` 内以 `map[string]Item` 作为 ID 主索引（Enqueue/Cancel 均为 O(1) 查找），单把 `sync.Mutex` 串行化所有公开方法，保证线性一致。`Pop`/`Snapshot` 在取出后按 Priority 降序、ReadyAt 升序、ID 升序排序（O(n log n)）。
- `validation.go`：无副作用批次预检。`ValidateBatch` 只读取不可变配置（上限），做完整结构校验（时间非负、kind 合法、ID 字符集与字节上限、Cancel 不得携带额外字段），与 `Apply` 共享同一结构语义。
- `stats.go`：`Stats` 在同一互斥锁临界区内读取 generation、nextRevision、逻辑时钟与条目数，是线性一致的状态摘要。
- `clone.go`：`Clone` 在锁内深拷贝全部条目与逻辑时钟（now/generation/nextRevision），副本与原对象所有权完全隔离，互不影响。
- `preview.go`：`Preview` 在锁内取一次线性化快照并克隆出候选队列，在候选上复用完整的 `Apply` 事务语义，返回候选 `Result`、`Snapshot`、`Stats`；原对象的状态、generation、revision 与逻辑时钟均不变，错误及优先级与同状态上的 `Apply` 完全一致，失败时全部返回零值。

## 候选事务与回滚

`Apply` 先结构校验，再检查单调时间（`ErrTime`），随后在条目副本上顺序执行 Enqueue（分配 revision）/Cancel；任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）都直接丢弃副本，时间、状态与 revision 全部回滚。非空成功批次 generation 只增一次，空批次不变。

## 所有权

所有返回的切片（`Pop`、`Snapshot`、`Preview`）均为新分配的拷贝，调用方修改不会影响队列内部状态；`Clone` 的 map 也是全新分配。

## 复杂度

- `Apply`：O(n + k)，n 为当前条目数（事务副本），k 为批次操作数。
- `Pop` / `Snapshot`：O(n log n)（排序）。
- `Stats`：O(1)；`Clone` / `Preview`：O(n)（外加候选事务本身的开销）。
- `ValidateBatch`：O(k)，不读状态、无副作用。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
