# readyqueue295

并发安全的内存型“就绪优先队列”，仅依赖标准库（Go 1.22+）。语义见 `SPEC.md`。

## 多文件架构

- `prioritybox.go` — 核心事务引擎：`New` / `Apply` / `Pop` / `Snapshot`，以及规范排序（Priority 降序、ReadyAt 升序、ID 升序）。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`，与 `Apply` 共享同一个 `checkBatch`，保证预检与事务语义完全一致。
- `stats.go` — `Stats` 在读锁下一次性采样所有逻辑时钟与条目数，提供线性一致的状态摘要。
- `clone.go` — `Clone` 保留全部逻辑时钟（now / generation / nextRevision）并深拷贝索引，副本与原件所有权完全隔离。

## 索引

主索引是 `map[string]Item`，按 ID 提供 O(1) 的存在性判断（`ErrExists` / `ErrNotFound`）。弹出时不维护堆，而是在候选集合上按规范顺序排序，避免“最高优先级未就绪”时堆顶失效的问题。

## 候选事务

`Apply` 先在私有副本（拷贝的 map + 本地 revision 计数）上顺序执行 Enqueue/Cancel，仅在全部成功且最终容量检查通过后才一次性提交：替换索引、推进时间与 revision、generation 恰好加一。任何失败（`ErrTime` / `ErrExists` / `ErrNotFound` / `ErrCapacity`）直接丢弃候选副本，时间、状态与 revision 天然回滚。空批次不增加 generation。

## 所有权

`Pop` / `Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态；`Item` 为值类型，索引中不共享指针。`Clone` 逐条拷贝 map，后续对任一副本的写操作互不可见。

## 复杂度

设 N 为队列大小、B 为批次大小、R 为就绪候选数、K 为弹出上限：

- `Apply`：O(N + B)（候选拷贝 + 顺序执行），容量检查 O(1)。
- `Pop`：O(N + R log R)（筛选 + 排序），删除 O(K)。
- `Snapshot` / `Clone`：O(N log N) / O(N)。
- `Stats` / `ValidateBatch`：O(1) / O(B)，均不读取可变状态（预检）。

所有公开方法由单个 `sync.RWMutex` 保护，可并发调用；写操作互斥，读操作（`Snapshot` / `Stats` / `Clone`）可并行。
