# taskqueue150

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构（三层联动）

- **状态引擎 `prioritybox.go`**：拥有事务性数据与快照。单把 `sync.Mutex` 保护全部状态；
  `Apply` 先做完整结构校验（kind、ID 字符集与字节上限、非负时间），再在**候选事务**（克隆的
  `map[string]Item` 与候选 `nextRevision`）上顺序执行 Enqueue/Cancel，容量只在末尾检查，
  任何失败直接丢弃候选，时间、状态与 revision 天然回滚。
- **策略层 `policy.go`**：独立 `sync.RWMutex` 同步的 actor 白名单与单批操作数上限。
  `ReplaceActors` 先构建新 map 再一次赋值，实现原子替换；`Authorize` 只读策略，绝不触碰核心状态。
- **协调层 `coordinator.go`**：先 `Authorize`，通过后才调用引擎；每次尝试（成功、拒绝、
  引擎失败）都在私有锁下追加一条带连续 `Sequence` 的 `Decision` 审计记录。

## 索引与复杂度

- 主索引：`map[string]Item`，Enqueue/Cancel/存在性检查均摊 O(1)。
- `Apply`：O(n + k)，n 为当前任务数（克隆候选），k 为批内操作数。
- `Pop`：O(n + r log r)，扫描就绪任务后按 Priority 降序、ReadyAt 升序、ID 升序排序，
  截取 limit 并原子删除；未就绪任务保留。
- `Snapshot`：O(n log n)，返回按 ID 排序的副本。

## 所有权隔离

`Snapshot.Items`、`Pop` 结果与 `Coordinator.Decisions()` 均为新分配的切片，
调用方修改不会影响内部状态；候选事务在提交前与正式状态完全隔离。

## 并发安全

所有公开方法（`Queue`、`Policy`、`Coordinator`）均可被多个 goroutine 并发调用；
时间单调（`ErrTime` 拒绝回退），非空成功批次 generation 只增一次，revision 单调不回卷。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
