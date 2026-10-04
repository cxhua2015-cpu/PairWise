# quota

并发安全的内存多维配额管理器（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`，实现位于 `quota/quota.go`。

## 索引结构

管理器内部维护三张 map，由一把 `sync.Mutex` 保护：

- `limits`: `subject -> dimension -> limit`，已配置的配额上限。
- `used`: `subject -> dimension -> used`，由活跃 reservation 派生的当前用量（与 `limits` 同构，便于 O(1) 查找）。
- `reservations`: 全局唯一 `id -> {demands, metadata}`，demands 以 (subject, dimension) 排序的规范形式存储。

另维护 `metadataBytes`（已存 metadata 总字节）与 `generation`（单调递增代次）两个计数器。

## 配额核算

- 每个 demand 的占用检查为 `demand > limit - used`：`limit - used` 恒为非负且不会溢出，从而避免 `used + demand` 的 int64 溢出，溢出场景统一返回 `ErrExceeded`。
- `Replace` 先在候选视图中扣掉旧 demands 再校验新 demands，因此允许在同一 reservation 内跨维度搬移容量。
- 用量不单独持久化推导，快照中的 `Used` 直接来自 `used` 索引，与 reservation 集合始终一致。

## 事务语义

所有公开方法遵循同一事务管线：

1. **结构校验**：在加锁前完整校验全部输入（名称非空且不超过 `MaxNameBytes`、金额符号约束、(subject, dimension) 唯一性、metadata 单条上限），失败返回 `ErrInvalidInput`，不触碰任何状态。
2. **语义检查**：在持锁的隔离候选状态上执行（存在性、唯一性、配额与溢出），不就地修改。
3. **容量检查**：最后检查 subject 数、reservation 数与 metadata 总字节，失败返回 `ErrCapacity`。

任一阶段失败即整体回滚（状态与 generation 完全不变）；成功时所有变更一次性提交，`generation` 恰好推进一次。

## 容量

- `MaxSubjects` / `MaxReservations`：按候选集合大小在提交前检查。
- `MaxMetadataBytes`：既是单条 metadata 的结构上限，也是全局总字节上限；`Replace` 按 `total - old + new` 计算候选总量。

## 所有权

- 输入的 metadata 在存储前深拷贝，调用方之后修改输入切片不影响内部状态。
- 快照中的 demands 与 metadata 均为深拷贝，修改快照返回值不会影响内部状态或其他快照。
- 输入的 demands 在规范化（排序）时复制，不与内部状态共享内存。

## 复杂度

设 d 为单次调用的 demand/limit 数量，S 为 subject 数，D 为总维度数，R 为 reservation 数，M 为 metadata 总字节：

- `SetLimits`：时间 O(d)（含哈希去重），空间 O(d)。
- `Reserve` / `Replace`：时间 O(d log d)（规范化排序）+ O(d·k)（`Replace` 旧 demands 匹配，k 为旧 demand 数），空间 O(d)。
- `Release` / `DeleteSubject`：时间 O(该 reservation 的 demands) / O(R·d)，空间 O(1)。
- `Snapshot`：时间 O(S log S + D log D + R log R + M)，空间 O(D + R·d + M)。
- 总存储：O(subject 维度数 + reservation demands 数 + metadata 字节)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
