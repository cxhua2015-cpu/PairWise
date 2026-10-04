# idempotency

并发安全的内存幂等请求注册表（Go 1.22+，仅标准库）。公开契约见 `SPEC.md`。

## 状态机

每个 key 对应一条记录，处于两种状态之一：

- **pending**：`Begin` 首次创建（或过期后被接管）时进入，持有 `LeaseUntil` 租约截止时间与 fencing token。
- **completed**：`Complete` 成功后进入，保存深拷贝的结果与 `ReplayUntil`。

转换路径：

- 不存在 / 过期 pending / 过期 completed → `Begin` 创建或替换为 pending（leader）。
- pending 未过期且 fingerprint 相同 → `Begin` 观察为 `Pending`；不同 → `ErrConflict`。
- completed 未过期且 fingerprint 相同 → `Begin` 返回 `Replay` 及结果深拷贝；不同 → `ErrConflict`。
- pending → `Renew`（延长租约）/ `Complete`（转 completed）/ `Abort`（删除）。
- `Sweep(now, limit)` 删除 `now >= LeaseUntil` 的 pending 与 `now >= ReplayUntil` 的 completed，按 key 升序，`limit == 0` 表示不限。

## Fencing token

每次成功创建/替换 pending 记录分配一个非零、单调递增的 token。`Renew`/`Complete`/`Abort` 只接受当前 pending token；过期记录被新 leader 接管后，旧 token 一律返回 `ErrStaleToken`，防止旧 leader 误写。租约到期后（`now >= LeaseUntil`）即使 token 匹配，`Renew`/`Complete` 也返回 `ErrStaleToken`。

## 过期

过期完全由调用方传入的 `now` 驱动，注册表不使用墙钟。`Begin` 与 `Sweep` 在各自传入的时间点判定过期；`Sweep` 是唯一删除过期记录的途径（`Abort` 只删当前 pending）。completed 记录在 `ReplayUntil` 到期前一直可 replay，之后表现得如同不存在。

## 容量

容量只统计当前记录条数与保存的结果字节数：

- `MaxEntries`：记录条数上限；`Begin` 新增记录前检查，替换已有记录不占新额度，失败返回 `ErrCapacity` 且不消耗 token。
- `MaxResultBytes`：单个结果字节上限，`Complete` 超限返回 `ErrResultTooLarge`。

所有失败操作不改变状态、计数或 generation；每次成功的可观察状态变更只将 generation 推进一次（`Sweep` 仅在实际删除至少一条记录时推进）。

## 所有权与隔离

`Complete` 深拷贝传入的结果；`Begin` 的 replay 与 `Snapshot` 返回的所有字节切片均为独立副本，调用方修改返回值不会影响内部状态，反之亦然。

## 复杂度

实现使用单一互斥锁保护一个 map：

- `Begin` / `Renew` / `Complete` / `Abort`：平均 O(1)。
- `Sweep` / `Snapshot`：O(n log n)（按 key 排序），n 为记录数。
- 空间：O（记录数 + 结果字节数）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
