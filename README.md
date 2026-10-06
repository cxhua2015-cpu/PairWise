# readyqueue260

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构

实现刻意拆分为四个相互联动的组件，共享同一套语义：

- `readyqueue260/prioritybox.go` — 核心事务引擎：`New` / `Apply` / `Pop` / `Snapshot`。
- `readyqueue260/validation.go` — 无副作用的批次结构预检 `ValidateBatch`；`Apply` 在读取任何状态之前调用同一套校验，保证“先完整结构校验，再读取状态”。
- `readyqueue260/stats.go` — 线性一致的状态摘要 `Stats`，与并发事务在单一互斥点上保持一致。
- `readyqueue260/clone.go` — 深拷贝 `Clone`，保留逻辑时钟（now、generation、nextRevision）且完全隔离所有权。

## 索引

内部使用 `map[string]Item` 作为唯一权威索引（按 ID 寻址，O(1) 查找/删除）。不维护堆：`Pop` 与 `Snapshot` 在取数时按需过滤并排序（Priority 降序、ReadyAt 升序、ID 升序）。这一选择在容量有界（`Options.MaxItems`）的控制面场景下以最简单的方式保证正确性与可回滚性。

## 候选事务

`Apply` 先在 `ValidateBatch` 中做纯结构校验（非负 Now、合法 kind、ID 字符集与字节上限、Cancel 的 Priority/ReadyAt 必须为零），不读取任何状态。随后持锁检查时间单调性，并在**候选副本**（复制出的 map 与 revision 计数器）上顺序执行 Enqueue/Cancel：Enqueue 分配递增 revision，重复 ID 报 `ErrExists`，缺失 ID 报 `ErrNotFound`。最终容量只在所有操作执行完后检查一次（允许“先 Cancel 再 Enqueue”腾位）。任何失败都直接丢弃候选副本——时间、状态与 revision 天然回滚；成功才一次性提交并使 generation 恰好加一。空批次是只读操作，不改变 generation 与逻辑时钟。

## 所有权

所有公开方法（`Apply`/`Pop`/`ValidateBatch`/`Stats`/`Snapshot`/`Clone`）由同一把互斥锁保护，可并发调用。`Pop`、`Snapshot` 返回的切片均为新分配，与内部状态完全隔离；`Clone` 复制全部条目与逻辑时钟，克隆体与原队列互不影响。`Options` 在 `New` 之后不可变，因此无锁校验也是并发安全的。

## 复杂度

设 n 为队列中条目数，b 为批次操作数：

- `Apply`：时间 O(n + b)（复制候选 map + 顺序执行），空间 O(n)。
- `Pop(k)`：O(n log n) 排序后取前 k 个并删除。
- `Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)；`ValidateBatch`：O(b·L)，L 为 ID 长度。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
