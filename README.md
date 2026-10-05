# resourceledger102

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
账户主索引为 `map[string]Account`（按名称 O(1) 定位）。`Top` 与 `Snapshot`
不维护额外有序索引，而是在读路径上对当前账户快照排序：`Top` 按值降序、
名称升序，`Snapshot` 按名称升序。由于 `Account` 为值类型，返回的切片与
内部状态天然隔离，调用方修改不影响账本。

### 候选事务
`Apply` 先做整批结构校验（kind、名称字符集与字节上限），不触碰状态；
随后在写锁内把账户表克隆为候选副本，按输入顺序在副本上执行
Add/Set/Delete。Add 在算术前检测 int64 溢出，Add/Set 均执行绝对值上限；
最终账户容量仅在批次末检查。任一步失败直接丢弃候选副本，实现整体回滚；
成功时原子换入副本。非空成功批次 generation 恰好加一，空批次不变。

### 所有权
`Ledger` 内部状态（map、generation、nextRevision）完全私有，仅通过
`sync.RWMutex` 保护：写操作（`Apply`）持写锁，读操作（`Top`、`Snapshot`）
持读锁，可并发执行。所有返回的切片均为新建副本，所有权移交调用方。

### 复杂度
设 n 为账户数、k 为批内操作数：
- `Apply`：时间 O(n + k)（克隆候选副本 + 顺序执行），空间 O(n)。
- `Top`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
