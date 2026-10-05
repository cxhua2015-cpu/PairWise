# resourceledger142

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 三层架构

- **状态引擎 `creditpool.go`**：`Ledger` 持有事务性数据。`Apply` 在单把 `sync.RWMutex` 写锁内先完成整批结构校验，再在暂存区（staged map）按输入顺序执行 Add/Set/Delete，最后一次性提交；任何错误直接丢弃暂存区，实现整体回滚。`Top`/`Snapshot` 使用读锁。
- **策略层 `policy.go`**：`Policy` 拥有独立互斥锁保护的可原子替换 actor 白名单（`ReplaceActors` 整体换入新 map）及单批操作数上限；`Authorize` 只读策略状态，绝不触碰核心数据。
- **协调层 `coordinator.go`**：`Coordinator.Apply` 先 `Authorize`，通过后才委托 `Ledger.Apply`；无论成功、拒绝或引擎失败，都在独立互斥锁下追加一条带连续序号（从 1 起）的 `Decision` 审计记录。`Decisions()` 返回副本切片，不别名内部存储。

## 索引与候选事务

- 主索引为 `map[string]Account`（按名称 O(1) 定位）；无二级索引，`Top`/`Snapshot` 在读取时拷贝后排序。
- 候选事务（候选写）以 `staging map[string]*staged` 形式在批次内累积，容量检查（`MaxAccounts`）仅在批次末对最终账户数执行，提交前失败即整体回滚，revision 与 generation 均不推进。
- Add/Set 在提交时分配连续 revision；非空成功批次 generation 恰好 +1，空批次不变。

## 所有权隔离

`Result.Changed`、`Top`、`Snapshot.Accounts`、`Decisions()` 均返回新分配的切片/值拷贝，调用方修改不会影响内部状态；`ReplaceActors` 将入参拷贝进新 map，调用方之后修改入参切片不影响策略。

## 复杂度

- `Apply`：O(k·n)（k 为批内操作数，n 为被触及账户数；结构校验与暂存均为线性，最终容量统计 O(n)）。
- `Top`：O(a log a)，a 为当前账户数。
- `Snapshot`：O(a log a)（按名称排序）。
- `Authorize`/`ReplaceActors`：O(1) / O(m)（m 为 actor 数）。
- `Coordinator.Apply`：策略 + 引擎开销外加 O(1) 审计追加；`Decisions` 为 O(d) 拷贝。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
