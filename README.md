# balanceledger247

并发安全的内存型余额账本，仅依赖 Go 标准库。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件：

- `creditpool.go` — 核心事务引擎：`New` / `Apply` / `Top` / `Snapshot`，持有互斥锁、账户索引与逻辑时钟。
- `validation.go` — 无副作用的结构预检：`ValidateBatch` 与 `Apply` 共享同一个 `validateBatch`，保证“先完整结构校验，再读取状态”。
- `stats.go` — 线性一致统计：`Stats` 在读锁下返回 generation、next revision 与账户数。
- `clone.go` — 深拷贝：`Clone` 复制全部账户与逻辑时钟，所有权完全独立。

## 索引

账户存储为 `map[string]Account`，按名称 O(1) 定位；`Top` 与 `Snapshot` 在读取时物化并排序副本，不维护有序索引，避免写路径上的额外开销。

## 候选事务

`Apply` 先在 `validateBatch` 中做纯结构校验（kind、名称字符集与字节上限、字段纪律、Set 值域），不触碰状态。随后在写锁内把整批操作暂存到按名称索引的候选区（staged copy），按输入顺序执行 Add/Set/Delete：

- Add 在算术前检测 int64 溢出，再对结果执行绝对值上限（`ErrValue`）。
- Add/Set 各分配一个连续 revision；Delete 要求账户存在（`ErrNotFound`）。
- 最终账户容量仅在批次末检查（`ErrCapacity`），因此批内“先删后建”可以成功。
- 任一步失败直接返回，候选区被丢弃，已提交状态与逻辑时钟完全不变（整体回滚）。

全部成功后一次性提交候选区；非空批次 generation 恰好加一，空批次不改变任何时钟。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新分配的副本，调用方修改不会影响内部状态。`Clone` 逐账户复制 map，克隆体与原账本互不影响，并保留 generation 与 revision 逻辑时钟。

## 并发与复杂度

单个 `sync.RWMutex` 保护全部状态：`Apply` 取写锁，`Top` / `Snapshot` / `Stats` / `Clone` 取读锁，`ValidateBatch` 无锁（不读可变状态）。

- `Apply`：O(k)，k 为批内操作数（外加候选区内存 O(k)）。
- `Top`：O(n log n)，n 为账户数；`Snapshot`：O(n log n)（按名称排序）。
- `Stats`：O(1)；`Clone`：O(n)；`ValidateBatch`：O(k·L)，L 为名称长度。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
