# resourceledger112

并发安全的内存型资源计量账本。原子批次按输入顺序执行 `Add`/`Set`/`Delete`，
失败整体回滚。仅依赖标准库，需要 Go 1.22+。语义细节见 `SPEC.md`。

## 索引

- 主索引：`map[string]Account`，按账户名 O(1) 定位，键即规范名称
  （非空 ASCII 小写字母/数字/连字符/下划线，长度受 `MaxNameBytes` 约束）。
- 无次级排序索引：`Top` 与 `Snapshot` 在读取时对当前账户快照按需排序，
  以换取写入路径 O(1) 与实现的简单性。

## 候选事务（candidate transaction）

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态前校验整个批次（kind 合法、名称合法），
   失败返回 `ErrInvalidInput`。
2. **暂存执行**：在仅覆盖被触及账户的 staging 映射上按序执行操作。
   `Add`/`Set` 在算术前检测 int64 溢出并执行 `MaxAbsValue` 绝对值上限
   （`ErrValue`）；`Delete` 要求账户存在（`ErrNotFound`）。账户容量
   `MaxAccounts` 仅在批次末对最终账户数检查（`ErrCapacity`）。
   任何失败直接丢弃 staging，主状态零改动，实现整体回滚。

成功提交时把 staging 写回主映射：非空批次 `generation` 恰好加一，
每个 `Add`/`Set` 分配一个连续递增的 `revision`，`Delete` 不消耗 revision。

## 所有权与并发

- 所有公开方法由一把 `sync.Mutex` 保护，可并发调用；`Apply` 的
  校验—暂存—提交整体在临界区内，批次之间满足串行化语义。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的
  副本，调用方修改不会影响账本内部状态，反之亦然。

## 复杂度

设批次含 `k` 个操作、触及 `t` 个不同账户、账本共 `n` 个账户：

- `Apply`：时间 O(k + t)，额外空间 O(t)。
- `Top(m)`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
