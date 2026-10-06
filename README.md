# balanceledger222

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义：

- `creditpool.go` — 核心事务引擎：`New` / `Apply` / `Top` / `Snapshot`，以及 `Ledger` 状态（账户表 + `generation` / `nextRevision` 逻辑时钟）。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 与 `Apply` 共用同一个 `validate`，只做结构校验（kind、名称字符集与字节上限、各 kind 的冗余字段、操作数绝对值上限），不读取账户状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁内一次性快照 generation、nextRevision 与账户数。
- `clone.go` — 深拷贝：`Clone` 复制全部账户与逻辑时钟，所有权完全独立。

## 索引

账户存储为 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot` 在读取时物化切片并排序，不维护有序索引——账户数适中时这是最简单且正确的设计，写路径保持 O(1)。

## 候选事务（staging）

`Apply` 先在 `validate` 中对整个批次做完整结构校验，再在写锁内把账户表复制到候选 map 上按输入顺序执行 Add/Set/Delete：

- Add 在算术前检测 int64 溢出，再对结果执行绝对值上限（`ErrValue`）。
- Add/Set 每次分配连续 revision；Delete 不分配。
- 最终账户容量仅在批次末检查（`ErrCapacity`），允许批内先超后降。
- 任何失败直接丢弃候选 map，原状态、revision 与 generation 完全不变（整体回滚）；成功时一次性换入，`generation` 仅对非空批次加一。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新分配的副本，调用方修改不会影响内部状态；`Clone` 逐条复制账户，克隆体与原账本互不影响。

## 并发与复杂度

单把 `sync.RWMutex` 保护全部状态：写操作（`Apply`）独占，读操作（`Top` / `Snapshot` / `Stats` / `ValidateBatch` / `Clone`）共享。设批次含 k 个 op、账户数为 n：

- `Apply`：O(n + k)（候选复制 + 顺序执行）
- `ValidateBatch`：O(k)
- `Top`：O(n log n)，`Snapshot`：O(n log n)
- `Stats`：O(1)，`Clone`：O(n)

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
