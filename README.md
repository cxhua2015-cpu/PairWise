# balanceledger287

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 架构

实现按职责拆分为四个文件，共享同一套结构语义：

- `creditpool.go` — 核心事务引擎：`New` / `Apply` / `Top` / `Snapshot`，以及溢出安全算术。
- `validation.go` — `ValidateBatch`：无副作用的批次结构预检（kind、名称字符集与字节上限、未使用字段必须为零、`Add` 的 Delta 非零、`Set` 的绝对值上限）。`Apply` 在读取任何状态之前调用同一函数，保证预检与事务语义一致。
- `stats.go` — `Stats`：在 `RWMutex` 读锁下返回线性一致的 generation / nextRevision / 账户数。
- `clone.go` — `Clone`：在读锁下深拷贝全部账户与逻辑时钟（generation、nextRevision），副本与原账本完全隔离。

## 索引

账户存储为 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot` 在读取时物化并排序切片（分别为值降序/名称升序、名称升序），返回的是新建切片，调用方修改不会影响内部状态。

## 候选事务

`Apply` 先执行 `ValidateBatch` 结构预检，再在写锁内把当前账户表复制为候选 map，按输入顺序在其上应用 Add/Set/Delete：Add/Set 分配连续 revision，加法在算术前检测 int64 溢出并执行绝对值上限，Delete 缺失账户返回 `ErrNotFound`。最终账户容量仅在批次末检查。任一步失败直接丢弃候选 map，原状态、generation 与 revision 时钟完全不变（整体回滚）；成功时一次性提交候选 map，非空批次 generation 恰好加一，空批次不改变任何状态。

## 所有权

所有公开方法在单把 `sync.RWMutex` 下并发安全：写操作（`Apply`）持写锁，读操作（`Top`/`Snapshot`/`Stats`/`Clone`）持读锁。`Result.Changed`、`Top`、`Snapshot` 均返回新分配的切片，`Clone` 复制整个 map，不存在内部状态的别名外泄。

## 复杂度

- `ValidateBatch`：O(b)，b 为批次操作数；不读状态。
- `Apply`：O(n + b)，n 为当前账户数（候选复制）；回滚无额外代价。
- `Top(k)`：O(n log n) 排序后取前 k。
- `Snapshot`：O(n log n)（按名称排序）；`Stats`：O(1)；`Clone`：O(n)。
