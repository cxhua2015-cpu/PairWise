# balanceledger317

并发安全的内存型“余额账本 317”（Go 1.22+，仅标准库）。原子批次按输入顺序执行
`Add`/`Set`/`Delete`，失败整体回滚。详细语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- 不维护有序索引：`Top` 与 `Snapshot` 在读取时现收集现排序，写路径保持 O(1)。
- 单调计数器 `generation`（每个非空成功批次 +1）与 `revision`（每个 `Add`/`Set`
  分配一个连续值）保存在 `Ledger` 内。

### 候选事务（candidate transaction）
`Apply` 分两阶段：
1. **结构校验**：先完整校验所有 op 的 kind 与名称（非空、字节上限、仅
   `[a-z0-9-_]`），任何 `ErrInvalidInput` 在读取状态之前返回。
2. **执行 + 撤销日志**：在写锁内按序执行。每个账户首次被触碰时记录其旧值到
   undo log；`Add` 在算术前检测 int64 溢出，随后执行绝对值上限
   （`MaxAbsValue`）检查；`Delete` 缺失账户返回 `ErrNotFound`。任一步失败即按
   逆序回放 undo log 并恢复 revision 计数器，实现整体回滚。账户容量
   （`MaxAccounts`）只在批次末检查一次，因此“先删后建”的批次可以成功。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的拷贝，
  调用方修改不会影响内部状态，内部状态也不会逃逸到调用方。

### 复杂度
- `Apply`：O(k)，k 为批次内 op 数（回滚同为 O(k)）。
- `Top`：O(n log n)，n 为账户数（全量排序后取前 m 个）。
- `Snapshot`：O(n log n)（按名称排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
