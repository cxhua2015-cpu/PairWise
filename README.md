# readyqueue240

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Item`（按 ID 精确查找），`Enqueue`/`Cancel` 为 O(1)。
- `Pop`/`Snapshot` 按需对候选集做规范排序（Priority 降序、ReadyAt 升序、ID 升序），
  不维护额外的堆结构，避免删除任意元素时的堆修复成本；队列规模受 `MaxItems` 约束，
  排序开销可控。

## 候选事务

- `Apply` 先在无锁下做完整结构校验（与 `ValidateBatch` 共享 `validateBatchStructure`），
  再在互斥锁内把整批操作顺序应用到**候选副本**（map 拷贝 + 候选 revision 计数器）。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，时间、状态与
  revision 天然回滚；容量只在全部操作执行完后检查一次。
- 全部成功才一次性提交：替换 map、推进 `now` 与 `nextRevision`，非空批次 `generation` 仅 +1。

## 所有权

- 所有公开方法经单一互斥锁串行化，`Stats`/`Snapshot`/`Clone` 在锁内读取，保证线性一致。
- `Snapshot`/`Pop` 返回的切片与内部状态完全隔离；`Clone` 深拷贝全部条目与逻辑时钟
  （`now`/`generation`/`nextRevision`），克隆体与原队列互不影响。

## 复杂度

- `Apply`：O(k·n)，k 为批内操作数（候选 map 拷贝 O(n) + 每操作 O(1)）。
- `Pop`：O(n log n)（筛选就绪项并排序，删除至多 n 项）。
- `Snapshot`/`Clone`：O(n log n) / O(n)；`Stats`：O(1)。

## 文件划分

- `readyqueue240/prioritybox.go` — 核心类型与事务引擎（`New`/`Apply`/`Pop`/`Snapshot`）。
- `readyqueue240/validation.go` — 无副作用的结构预检（`ValidateBatch`）。
- `readyqueue240/stats.go` — 线性一致统计（`Stats`）。
- `readyqueue240/clone.go` — 保留逻辑时钟的深拷贝（`Clone`）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
