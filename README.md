# balanceledger357

并发安全的内存型“余额账本”。语义见 `SPEC.md`：原子批次按输入顺序执行
Add/Set/Delete，Add/Set 分配连续 revision，算术前检测 int64 溢出并执行绝对值
上限，账户容量仅在批次末检查，失败整体回滚。

## 设计说明

### 索引

- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- 不维护持久有序结构：`Top` 与 `Snapshot` 在调用时把 map 值拷贝到新切片并
  排序（`Top` 按值降序、名称升序；`Snapshot` 按名称升序）。账户数受
  `MaxAccounts` 上限约束，按需排序的代价可控，且避免在写路径上维护冗余索引。

### 候选事务

`Apply` 分两阶段：

1. **结构校验**：先完整校验所有 op 的 kind 与名称合法性，不触碰任何状态，
   也不消耗 revision。
2. **执行 + 回滚日志**：直接在主索引上应用操作，同时记录 undo 日志
   （每个被触及账户的旧值及是否存在）。任一步失败（溢出/绝对值上限
   `ErrValue`、删除不存在账户 `ErrNotFound`、批次末容量 `ErrCapacity`）时
   逆序回放日志恢复原状，revision 计数器与 generation 均不前进。
   只有非空成功批次才会使 generation 恰好加一。

### 所有权与并发

- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；批次之间串行化，
  单个批次是原子的。
- 返回值（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建切片与值类型
  `Account` 的拷贝，调用方修改不影响内部状态，内部后续变更也不影响已返回
  的快照。

### 复杂度

设 B 为批次大小、N 为当前账户数：

- `Apply`：时间 O(B)，回滚日志空间 O(B)。
- `Top(k)`：时间 O(N log N)，空间 O(N)。
- `Snapshot`：时间 O(N log N)，空间 O(N)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
