# readyqueue235

并发安全的内存型“就绪优先队列”，仅依赖标准库（Go 1.22+）。

## Multi-file architecture

实现按职责拆分为四个相互联动的文件，共享同一套语义：

- `prioritybox.go` — 核心事务引擎：`New`/`Apply`/`Pop`/`Snapshot`、排序与回滚。
- `validation.go` — 无副作用的批次结构预检：`ValidateBatch` 与 `Apply` 共用同一套
  结构校验（`Apply` 第一步即调用它），不读取、不修改任何队列状态。
- `stats.go` — 线性一致的状态统计：`Stats` 在同一临界区内读取全部计数器。
- `clone.go` — 深拷贝：`Clone` 复制全部条目与逻辑时钟（now/generation/nextRevision），
  克隆体持有独立的互斥锁与 map，与原件完全隔离。

## 索引与数据结构

- 主索引为 `map[string]Item`，按 ID O(1) 定位，天然支持 Enqueue 查重与 Cancel 删除。
- 弹出时不维护堆，而是全量扫描后按 (Priority 降序, ReadyAt 升序, ID 升序) 排序取前
  `limit` 个；队列规模受 `MaxItems` 上限约束，排序实现简单且无堆修复开销。
- `Snapshot`/`Pop` 返回的切片均为新分配的副本，与内部状态完全隔离。

## 候选事务（Apply）

1. 结构预检（`ValidateBatch`）：`Now >= 0`、kind 合法、ID 为非空小写 ASCII
   `[a-z0-9-_]` 且不超过 `MaxIDBytes`、Enqueue 的 `ReadyAt >= 0`、Cancel 的
   `Priority`/`ReadyAt` 必须为 0；任何违例返回 `ErrInvalidInput`。
2. 加锁后检查单调时间：`Now < 当前 now` 返回 `ErrTime`。
3. 顺序执行 Ops：Enqueue 分配递增 revision（重复 ID 报 `ErrExists`），Cancel 删除
   （缺失报 `ErrNotFound`）；每步记录 undo 日志。
4. 最终容量检查：仅当全部 Ops 执行完后 `len(items) > MaxItems` 才报 `ErrCapacity`，
   因此“先 Cancel 再 Enqueue”的替换批次可以成功。
5. 任一步失败即按 undo 日志逆序回滚条目、revision 与时间，状态与调用前完全一致；
   成功且非空的批次 `generation` 恰好加一并推进 `now`，空批次不改变任何状态。

## 所有权与并发

- 所有公开方法通过单把 `sync.Mutex` 串行化，可任意并发调用；`Stats`/`Snapshot`
  在同一临界区构造，保证线性一致。
- `Clone` 在锁内复制 map 与逻辑时钟，返回独立锁的新队列；对克隆体的 Pop/Apply
  不影响原件，反之亦然。
- 返回的 `Item` 为值类型，切片均为新副本，调用方修改不会泄漏进队列。

## 复杂度

- `Apply`：O(k)，k 为批次内 Ops 数（不含最终 `len` 检查，O(1)）。
- `Pop`：O(n log n)，n 为就绪候选数（扫描 O(n) + 排序）。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)；`ValidateBatch`：O(k·L)，
  L 为 ID 长度。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
