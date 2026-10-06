# readyqueue285

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。

## 语义

- 显式非负单调时间：`Apply`/`Pop` 携带 `now`，负值返回 `ErrInvalidInput`，回退返回 `ErrTime`。
- `Apply` 原子顺序执行 Enqueue/Cancel：Enqueue 分配递增 revision，Cancel 删除；最终容量只在所有操作执行完后检查，超限返回 `ErrCapacity`。
- 任何失败整体回滚：时间、条目与 revision 计数保持不变。
- 非空成功批次 generation 恰好加一；空批次不改变 generation。
- `Pop(now, n)` 选取 `ReadyAt <= now` 的条目，按 Priority 降序、ReadyAt 升序、ID 升序原子删除并返回。
- 键仅允许非空 ASCII 小写字母、数字、连字符、下划线，且不超过 `Options.MaxIDBytes`；Cancel 不得携带 Priority/ReadyAt 等额外字段；未知 kind 返回 `ErrInvalidInput`。

## 实现说明

### 索引

主索引为 `map[string]Item`（按 ID 精确查找/去重，O(1)）。规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在读取路径（`Pop`/`Snapshot`）上按需排序产生，不维护额外的有序结构，从而保持事务路径简单。

### 候选事务

`Apply` 先在私有副本（candidate map）上顺序执行全部操作并推进本地 revision 计数，全部成功且最终容量检查通过后才一次性提交（替换 map、推进时间与计数）。任一失败直接丢弃候选状态，天然实现时间、状态、revision 的整体回滚。

### 所有权

- `Snapshot`/`Pop` 返回的切片均为新建副本，与内部状态完全隔离。
- `Clone` 深拷贝全部条目与逻辑时钟（now、generation、nextRevision），克隆体与原队列不共享任何内存。
- 所有公开方法通过单一互斥锁串行化，`Stats` 在锁内读取，保证线性一致。

### 复杂度

设 n 为队列条目数，b 为批次操作数：

- `Apply`：O(n + b)（复制候选 map + 顺序执行）。
- `Pop`：O(n log n)（筛选就绪条目并排序）。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。
- `ValidateBatch`：O(b)，无副作用、不读取状态。

## 文件分工

- `prioritybox.go`：类型、错误值、核心事务（Apply/Pop/Snapshot）。
- `validation.go`：无副作用的批次结构预检，与 `Apply` 共享同一套结构语义。
- `stats.go`：线性一致的状态统计。
- `clone.go`：保留逻辑时钟、所有权完全隔离的深拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
