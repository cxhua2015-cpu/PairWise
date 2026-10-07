# balanceledger412

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，Add/Set 分配连续 revision；算术前检测 int64 溢出并执行绝对值
上限，账户容量仅在批次末检查，失败整体回滚。`Top` 按数值降序、名称升序，
`Snapshot` 按名称排序。

## 多文件架构

- `creditpool.go` — 核心事务引擎：`Ledger` 状态、`Apply`/`Top`/`Snapshot`。
- `validation.go` — 无副作用批次预检：`validateBatch` 同时服务 `Apply` 与
  `ValidateBatch`，保证两者共享同一套结构语义（名称字符集与字节上限、kind
  合法性、各 kind 禁止的额外字段、Add 非零 delta）。
- `stats.go` — 线性一致统计：`Stats` 在读锁内取数，始终对应事务历史中的单一
  时间点。
- `clone.go` — 深拷贝：`Clone` 保留 generation/nextRevision 逻辑时钟，账户
  映射整体复制，所有权完全隔离。

## 索引

账户存储为 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot` 不维护
有序索引，而在读锁内对当前账户快照排序——账户数即控制面规模时，这比维护
有序结构更简单且无写路径开销。

## 候选事务

`Apply` 先完整结构预检（不读状态），再在写锁内把已提交账户复制到候选映射，
按顺序在候选上执行全部操作；任何溢出、绝对值上限、`ErrNotFound` 或批次末
容量检查失败都会直接丢弃候选，已提交状态与逻辑时钟保持不变（整体回滚）。
仅在全部成功时将候选一次性交换为已提交状态，非空批次 generation 恰好 +1，
revision 连续分配。

## 所有权

所有公开方法在 `sync.RWMutex` 保护下执行，可并发调用。`Account` 为纯值类型，
`Top`/`Snapshot`/`Clone` 返回的切片与映射均为新建副本，调用方修改不会反写
内部状态；`Clone` 与原账本互不影响。

## 复杂度

设 n 为账户数、k 为批次数：

- `Apply`：O(n + k)（候选复制 + 顺序执行），回滚零额外成本。
- `ValidateBatch`：O(k)，不触碰状态。
- `Top`：O(n log n)；`Snapshot`：O(n log n)；`Stats`：O(1)；`Clone`：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
