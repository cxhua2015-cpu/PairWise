# balanceledger277

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构（多文件联动）

- `creditpool.go` — 核心事务引擎：`New`/`Apply`/`Top`/`Snapshot`，以及名称与绝对值校验辅助。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`；`Apply` 在读取任何状态前先调用它，二者共享同一套结构语义（kind 合法、名称字符集与字节上限、Add 的 Delta 非零）。
- `stats.go` — 线性一致的状态摘要 `Stats`（读锁下的 generation / nextRevision / 账户数快照）。
- `clone.go` — 保留逻辑时钟（generation、nextRevision）且所有权完全隔离的深拷贝 `Clone`。

## 索引与数据结构

- 账户存储为 `map[string]Account`（按名称 O(1) 定位），无额外有序索引；`Top` 与 `Snapshot` 在读锁内现取现排。
- 逻辑时钟：`nextRevision` 从 1 开始，仅 Add/Set 消耗连续 revision；非空成功批次使 `generation` 恰好 +1，空批次与失败批次不变。

## 候选事务（staging / 回滚）

`Apply` 先经 `ValidateBatch` 做纯结构预检，再在写锁内把当前账户表浅拷贝到候选 map 上按输入顺序执行全部操作：算术前检测 int64 溢出并执行绝对值上限（`ErrValue`），Delete 缺失账户报 `ErrNotFound`，最终账户容量只在批次末检查（`ErrCapacity`）。任何失败直接丢弃候选 map，原状态、revision 与 generation 完全不变，实现整体回滚；全部成功才一次性提交。

## 所有权与并发

- 所有公开方法经 `sync.RWMutex` 保护，可并发调用；`Top`/`Snapshot`/`Stats`/`Clone` 使用读锁。
- 返回值（`Result.Changed`、`Top`、`Snapshot` 的切片）均为新建拷贝，与内部状态隔离；`Clone` 复制整个 map，克隆体与原账本互不影响。

## 复杂度

- `ValidateBatch`：O(批次操作数 × 名称长度)，不读写账本状态。
- `Apply`：O(账户数 + 操作数)（候选拷贝 + 顺序执行）。
- `Top`：O(A log A)，A 为账户数；`Snapshot`：O(A log A)（按名称排序）。
- `Stats`：O(1)；`Clone`：O(A)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
