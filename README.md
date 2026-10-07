# balanceledger412

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.

- `creditpool.go` — 核心事务引擎：`New` / `Apply` / `Top` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检：`ValidateBatch` 与 `Apply` 共享同一套
  `validateBatchLocked` 结构语义（kind 合法、名称字符集与字节上限、Add/Set 操作数非零），
  预检不读取、不修改任何账本状态。
- `stats.go` — 线性一致的状态摘要：`Stats` 在读锁内一次性采集 generation、nextRevision
  与账户数，与并发事务状态保持一致。
- `clone.go` — 深拷贝：`Clone` 保留逻辑时钟（generation、nextRevision），并复制全部账户，
  与原账本完全隔离所有权。

## 并发与索引

- 单一 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Top`/`Snapshot`/`Stats`/`Clone`/
  `ValidateBatch` 持读锁，所有公开方法可并发调用。
- 账户主索引为 `map[string]Account`（按名称 O(1) 定位）；`Top` 与 `Snapshot` 在读取时
  物化并排序，不维护额外的有序索引，避免写路径的额外开销。

## 候选事务与回滚

`Apply` 先在候选副本（账户 map 的浅值拷贝）上按输入顺序执行 Add/Set/Delete：Add/Set
分配连续 revision，Add 在算术前检测 int64 溢出并对结果执行绝对值上限，Set 直接校验
绝对值上限，Delete 要求账户存在。最终账户容量仅在批次末检查。任何一步失败直接丢弃
候选副本，原状态零改动，实现整体回滚；只有全部成功才一次性提交，非空成功批次
generation 恰好加一。

## 所有权

所有返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）都是新建并排序后的副本，
与内部 map 完全隔离；`Clone` 复制 map 与逻辑时钟，克隆体与原账本互不影响。

## 复杂度

- `Apply`：O(n + a)，n 为批内 op 数，a 为当前账户数（候选拷贝）。
- `ValidateBatch`：O(n)，纯结构校验。
- `Top` / `Snapshot`：O(a log a) 排序。
- `Stats`：O(1)；`Clone`：O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
