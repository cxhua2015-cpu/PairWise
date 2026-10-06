# balanceledger227

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动的实现文件：

- `creditpool.go` — 核心事务引擎：`New` / `Apply` / `Top` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义（kind、名称字符集与字节上限、Delta 非零、输入绝对值上限）。
- `stats.go` — 线性一致的状态统计 `Stats`（读锁下采样 generation / nextRevision / 账户数）。
- `clone.go` — 保留逻辑时钟（generation、nextRevision）且所有权完全隔离的深拷贝 `Clone`。

## 索引

主索引是 `map[string]Account`（按名称 O(1) 定位）。`Top` 与 `Snapshot`
不加额外有序索引，而是在读锁内把 map 物化为切片后排序：Top 按数值降序、
名称升序；Snapshot 按名称升序。这避免了写路径维护有序结构的成本，
以读路径 O(n log n) 换取事务路径的 O(1) 单点更新。

## 候选事务（candidate transaction）

`Apply` 先调用 `ValidateBatch` 做完整结构校验（不读状态），再在写锁内把
当前账户 map 复制为候选工作副本，按输入顺序在其上执行 Add/Set/Delete：

- Add/Set 从 `nextRevision` 分配连续 revision；Delete 不消耗 revision。
- Add 在算术之前检测 int64 溢出，并对结果执行绝对值上限（`ErrValue`）。
- 账户容量上限仅在批次末对候选副本检查（`ErrCapacity`）。
- 任一步失败直接丢弃候选副本，状态、revision 时钟、generation 完全不变，
  即整体回滚；成功时才用候选副本原子替换主状态，非空批次 generation 加一。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）
都是每次调用新建并填充的副本，与内部 map 完全隔离；调用方修改返回值
不影响账本。`Clone` 复制全部账户与逻辑时钟，克隆体与原账本不共享任何
可变状态。`Options` 在 `New` 之后不可变，因此 `ValidateBatch` 无需加锁
即可安全并发执行。

## 并发与复杂度

单个 `sync.RWMutex` 保护全部可变状态：`Apply` 持写锁，`Top` / `Snapshot` /
`Stats` / `Clone` 持读锁，因此统计与克隆相对并发事务是线性一致的。

- `ValidateBatch`：O(b)，b 为批次操作数。
- `Apply`：O(n + b)，n 为当前账户数（候选副本复制）。
- `Top` / `Snapshot`：O(n log n)。
- `Stats`：O(1)；`Clone`：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
