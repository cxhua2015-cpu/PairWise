# metacatalog296

并发安全的内存型元数据目录，仅依赖标准库（Go 1.22+）。原子批次按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；失败回滚全部状态、generation 与 revision。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`New`/`Apply`/`Get`/`Snapshot`，存储与索引。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义（kind、名称字符集与长度、Value 规则），不读取、不修改任何状态。
- `stats.go` — 线性一致的状态统计：`Stats` 在读锁下聚合记录数与 Value 总字节。
- `clone.go` — 保留逻辑时钟（generation、nextRevision）的深拷贝，所有权完全隔离。

## 索引

记录存储于 `map[string][]byte`（名称 → 拥有所有权的 Value 副本），revision 存于独立的 `map[string]uint64`。名称即主键，查找 O(1)；`Snapshot`/`Result.Changed` 按需对名称排序，不维护额外的有序结构。

## 候选事务

`Apply` 先通过 `ValidateBatch` 做完整结构校验（先于任何状态读取），然后在写锁内把当前两个 map 浅拷贝为候选状态，按输入顺序在候选上执行 Put/Delete。记录数与 Value 总字节容量只在批次末对候选检查；任何失败直接丢弃候选，原始状态、generation、revision 完全不变。成功时一次性提交候选，非空批次 generation 恰好加一。

## 所有权

所有进入（Put 的 Value）与离开（`Get`、`Snapshot`、`Result.Changed`、`Clone`）的字节切片都做深拷贝，调用方与 Store 之间、Clone 与原 Store 之间不共享任何可变内存；返回的切片与内部状态完全隔离。

## 并发与复杂度

单把 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Get`/`Snapshot`/`Stats`/`Clone` 取读锁，所有公开方法可并发调用且各自线性一致。

- `Get`：O(L)，L 为 Value 长度（深拷贝）。
- `Apply`：O(B·(N+V))，B 为批次大小，N 为记录数，V 为 Value 总字节（候选拷贝）。
- `Snapshot`/`Clone`：O(N log N + V) / O(N + V)。
- `Stats`：O(N)。
