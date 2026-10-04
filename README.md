# quota

并发安全的内存多维配额管理器（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 索引结构

`Manager` 使用单个 `sync.Mutex` 保护三张哈希表：

- `limits`: `subject -> dimension -> limit`，维度上限。
- `usage`: `subject -> dimension -> used`，由活跃 reservation 派生的当前用量（空维度/空 subject 即时清理）。
- `reservations`: 全局唯一 `id -> {demands, metadata}`，demands 以 (subject, dimension) 排序的规范形式存储。

另维护 `metadataBytes`（已存 metadata 总字节）与 `generation`（每次成功变更 +1）。

## 配额核算

- 每次 Reserve/Replace 成功后立即把 demands 累加进 `usage`，Release 时扣减；快照中的 `Used` 直接读取该表，保证与活跃 reservation 一致。
- 占用检查用 `demand > limit - used` 判定，由于不变量 `0 <= used <= limit` 恒成立，减法不会下溢，也避免了 `used + demand` 的 int64 溢出问题。
- 同一调用内重复的 `(Subject, Dimension)` 在结构校验阶段即被拒绝，因此单次扣减/累加无需考虑自重叠。

## 事务与校验顺序

所有复合操作分三段执行，任一失败整体回滚、状态与 generation 完全不变：

1. **结构校验**（不加锁）：名称非空且 ≤ `MaxNameBytes`、金额符号合法、demand/limit 列表非空且无重复对、metadata ≤ `MaxMetadataBytes`；失败返回 `ErrInvalidInput`。
2. **语义检查**（持锁，在隔离候选状态上）：`Replace` 先从 `usage` 移除旧 demands 再校验新 demands（允许容量在维度间搬移），失败则把旧 demands 加回完成回滚；未知维度 `ErrNotFound`、超限/溢出 `ErrExceeded`、重复 ID `ErrExists`。
3. **容量检查**（最后）：subject 总数、reservation 总数、metadata 总字节，失败返回 `ErrCapacity`。

只有全部通过才提交变更，且 `generation` 恰好推进一次（包括把上限设为相同值）。

## 容量

`New` 要求 `MaxSubjects / MaxReservations / MaxMetadataBytes / MaxNameBytes` 均为正，否则 `ErrInvalidOptions`。容量检查严格发生在所有语义检查之后，因此容量失败不会掩盖配额或存在性错误，也不会留下部分变更。

## 所有权隔离

- 存入时：metadata 深拷贝，demands 复制并规范排序，调用方之后修改输入切片不影响内部状态。
- 读出时：`Snapshot` 中的 demands 与 metadata 均为新分配的副本，多次快照之间、快照与内部状态之间完全独立。

## 并发

所有公开方法可并发调用。写操作持互斥锁串行执行；`Snapshot` 在锁内一次性构建一致快照。

## 复杂度

- `SetLimits`: 时间 O(L)，L 为批量大小（校验 + 逐条拟合检查）。
- `Reserve` / `Replace`: 时间 O(d log d)（d 为 demands 数，排序主导），其余为哈希表 O(d)。
- `Release`: O(d)。`DeleteSubject`: O(总 demands 数)（需扫描是否被引用）。
- `Snapshot`: O(n log n)，n 为 subject + dimension + reservation 总数（排序主导）。
- 空间：O(subject 维度数 + reservation demands 数 + metadata 字节数)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
