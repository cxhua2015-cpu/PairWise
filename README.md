# readyqueue240

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义与状态：

- `prioritybox.go` — 核心事务引擎：`New`/`Apply`/`Pop`/`Snapshot` 与规范排序。
- `validation.go` — 无副作用的批次结构预检；`ValidateBatch` 与 `Apply` 调用同一个
  `validateBatch`，保证预检与事务的校验语义完全一致。
- `stats.go` — `Stats` 在同一把互斥锁下读取，提供线性一致的状态摘要。
- `clone.go` — `Clone` 深拷贝全部状态（含逻辑时钟 now/generation/nextRevision），
  与原队列完全隔离所有权。

## 索引设计

- 主索引为 `map[string]Item`，Enqueue/Cancel 按 ID O(1) 定位。
- 不维护额外堆结构：Pop/Snapshot 在候选切片上按需排序
  （Priority 降序、ReadyAt 升序、ID 升序），避免辅助索引与主索引不一致。

## 候选事务

`Apply` 先对整个批次做完整结构校验（不读状态），再在锁内检查单调时间，
随后在 `maps.Clone` 出的候选 map 上顺序执行 Enqueue/Cancel：Enqueue 分配
递增 revision，Cancel 删除条目。容量上限只在所有操作执行完后检查。
任一步失败直接丢弃候选状态，时间、条目与 revision 全部回滚；
非空成功批次 generation 恰好加一，空批次不改变任何状态。

## 所有权

- 队列内部只持有值的私有副本；`Snapshot`/`Pop` 返回的切片均为新建，
  调用方修改不会影响内部状态。
- `Clone` 复制全部字段与 map，两个队列之后的演化互不影响。

## 复杂度

- `Apply`：O(k·n)，k 为批次操作数（候选 map 克隆 O(n)，每操作 O(1)）。
- `Pop`：O(n + r log r)，r 为就绪候选数（扫描 + 排序）。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。
- 所有公开方法在单把互斥锁下线性化，支持并发调用。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
