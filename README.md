# balanceledger257

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 架构

实现刻意拆分为四个相互协作的文件：

- `creditpool.go` — 核心事务引擎：`Ledger` 状态、`Apply`/`Top`/`Snapshot`。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`；`Apply` 复用同一套
  `validateBatch` 语义，保证预检与执行完全一致。
- `stats.go` — 线性一致的状态摘要 `Stats`（在读锁下一次性采样）。
- `clone.go` — 保留逻辑时钟（generation / nextRevision）且所有权完全隔离的深拷贝 `Clone`。

## 索引

账户存储为 `map[string]Account`（按名称 O(1) 定位）。不维护有序索引：
`Top` 与 `Snapshot` 在读锁保护下现取现排。`Snapshot` 按名称升序；
`Top` 按数值降序、名称升序（稳定决胜）。

## 候选事务

`Apply` 先在无锁状态下做完整结构校验，再在写锁内把整批操作应用到
`staged`/`deleted` 暂存区（候选事务）。任何一步失败（溢出、绝对值上限、
`ErrNotFound`、批次末容量检查）直接返回，已提交状态零改动，实现整体回滚。
全部通过后才一次性写回 map 并将 generation 递增一次；revision 按 Add/Set
操作顺序连续分配。空批次成功但不改变 generation。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为
新分配的副本，调用方修改不会影响内部状态。`Clone` 复制全部账户与逻辑时钟，
克隆体与原账本不共享任何可变状态。可变性由单个 `sync.RWMutex` 保护：
写路径（`Apply`）独占，读路径（`Top`/`Snapshot`/`Stats`/`Clone`）共享。

## 复杂度

- `Apply`：O(k)，k 为批内操作数（外加 O(k) 暂存空间）。
- `ValidateBatch`：O(k)，不读写账本状态。
- `Top(n)`：O(a log a)，a 为账户数。
- `Snapshot`：O(a log a)；`Stats`：O(1)；`Clone`：O(a)。
