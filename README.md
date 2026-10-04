# leasepool

并发安全、使用显式时间的内存加权租约池（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 索引结构

`Registry` 持有两张哈希索引，由一把 `sync.Mutex` 保护：

- `pools`: 池名 → `{capacity, used, leases}`，容量与用量按池独立核算。
- `leases`: 租约 ID → `Lease{Pool, Owner, Weight, ExpiresAt}`，租约 ID 全局唯一（跨池冲突返回 `ErrConflict`）。

## 批次事务（Apply）

1. **结构校验**：对全部操作按输入顺序做纯字段校验（名称/属主字母表与长度、权重为正、`ExpiresAt > Batch.Now`、Release/Renew 的零值字段约束、未知 Kind），不读任何状态；失败返回 `ErrInvalidInput`。
2. **时间检查**：`Batch.Now < 当前时间` 返回 `ErrTime`。
3. **隔离候选状态**：深拷贝 pools 与 leases 两张表，在副本上先回收所有 `ExpiresAt <= now` 的租约，再按输入顺序执行操作。任何失败（`ErrNotFound`/`ErrConflict`/`ErrCapacity`）直接丢弃副本——过期回收、时间、generation、用量全部不泄漏，实现整体回滚。
4. **提交**：成功时原子换入副本，`Now = Batch.Now`；若回收了租约或批次含至少一个操作，generation 恰好加一（仅推进时间的空批次不加）。`Result.Expired` 按字典序排序。

## 过期回收

租约在 `ExpiresAt <= now` 时到期（边界含等号）。`Apply` 在候选状态上、`Sweep` 在持锁状态上，先于任何操作执行回收；`Sweep` 仅当确有租约到期时 generation 加一，并推进时间。

## 容量核算

获取前检查 `weight > capacity - used`（减法形式，配合 `weight > 0` 与 `used <= capacity` 不变式，避免 uint64 加法溢出）；活租约数达到 `MaxLeases` 返回 `ErrCapacity`。释放/到期按租约权重精确扣减。

## 时间

注册表使用显式非负 `int64` 时间，从 0 开始；`Apply`/`Sweep` 要求 `now` 非递减，否则 `ErrTime` 且状态不变。

## 复杂度

- 结构校验：O(批次操作数 × 字段长度)。
- 候选克隆：O(池数 + 租约数)。
- 过期回收：O(租约数) 扫描 + O(k log k) 排序（k 为到期数）。
- 每个操作：O(1) 哈希读写；`Renew` 仅改 `ExpiresAt`。
- `Snapshot`：O(池数 log 池数 + 租约数 log 租约数)，池按名称、租约按 ID 排序，返回深拷贝。
- `Sweep`：O(租约数)。

## 验证

`go test ./...`、`go test -race ./...`、`go run ./cmd/demo` 全部通过。
