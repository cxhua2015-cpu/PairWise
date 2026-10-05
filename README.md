# resourceledger132

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。实现见 `SPEC.md`。

## 三层架构

- **状态引擎（`resourceledger132/creditpool.go`）**：`Ledger` 持有账户表、generation 与 revision 计数器。`Apply` 原子执行批次，`Top`/`Snapshot` 为只读查询。
- **策略层（`resourceledger132/policy.go`）**：`Policy` 维护可原子替换的 actor 白名单（`atomic.Value` 持有不可变 `map[string]struct{}`，整体替换，无锁读取）与单批操作数上限。`Authorize` 不触碰核心状态。
- **协调层（`resourceledger132/coordinator.go`）**：`Coordinator` 先调用 `Policy.Authorize` 做准入，再委托 `Ledger.Apply`，并为成功、拒绝与引擎失败分配连续审计序号（从 1 开始单调递增）。

## 索引与候选事务

- 核心索引为 `map[string]Account`（按名称 O(1) 定位）。`Top` 与 `Snapshot` 在读取时物化并排序，不维护有序索引，以保持写入路径 O(1)。
- `Apply` 采用**候选事务**：先在克隆的 map 上按输入顺序执行全部操作（Add/Set 分配连续 revision，Delete 不消耗 revision），算术前检测 int64 溢出并执行绝对值上限，最终账户容量仅在批次末检查；任一步失败即丢弃候选状态，整体回滚，generation 不变。只有非空成功批次才使 generation 恰好 +1。

## 所有权隔离

所有公开方法的返回切片（`Result.Changed`、`Top`、`Snapshot.Accounts`、`Coordinator.Decisions`）均为新分配的副本，不与内部存储共享底层数组；调用方修改返回值不影响账本或审计日志。`Policy.ReplaceActors` 将输入拷贝进新的不可变 map 后原子发布。

## 并发与复杂度

- `Ledger` 由 `sync.RWMutex` 保护：写（`Apply`）独占，读（`Top`/`Snapshot`）共享。
- `Policy` 白名单通过 `atomic.Value` 无锁替换与读取；`Coordinator` 用互斥锁保证审计序号连续且日志追加原子。
- 复杂度（n = 批次数，a = 账户数）：`Apply` 结构校验 O(n)，候选执行 O(n)，克隆 O(a)；`Top(k)` O(a log a)；`Snapshot` O(a log a)；`Authorize` O(1)；`Decisions` O(d)（d = 决策数）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
