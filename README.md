# rankboard

并发安全的内存加权排行榜（Go 1.22+，仅标准库）。实现见 `rankboard/rankboard.go`，规范见 `SPEC.md`。

## 事务语义

`Apply` 是原子的批量操作：

- 先完整校验整个批次（ID 合法性、Kind、数值字段约束），再读取任何状态。
- 操作按输入顺序作用于隔离的候选状态（当前条目的副本）。
- 条目容量（`MaxItems`）只在批次末尾检查一次，因此批次中途可以暂时超限。
- 任何失败整体回滚：条目、generation 和 revision 计数器都保持不变。
- 成功的非空批次使 generation 恰好加一；空批次不改变任何状态。
- Add/Set 各自分配一个连续递增的 revision；Delete 不分配。
- `Result.Changed` 包含批次结束时仍存活的、被 Add/Set 触及的条目，按 ID 排序去重。

## 排序

- `Top(limit)`（limit 为 1..1000）：按分数降序、ID 升序稳定排序，最多返回 limit 条。
- `Snapshot()`：返回全部条目，按 ID 升序。
- 返回的切片均为独立副本，修改不影响内部状态。

## 算术

- Add 先做 int64 溢出检测（在加法之前通过符号比较判定），再施加 `[-MaxAbsScore, MaxAbsScore]` 绝对值上限；违规返回 `ErrScore`。
- Set 在校验阶段即检查分数上限，越界返回 `ErrInvalidInput`。

## 并发与复杂度

所有公开方法通过 `sync.RWMutex` 并发安全：`Apply` 持写锁，`Top`/`Snapshot` 持读锁。

- `Apply`：O(k·n) 复制候选状态 + O(k) 执行 + O(k log k) 排序 Changed，k 为批大小，n 为条目数。
- `Top`：O(n log n)。
- `Snapshot`：O(n log n)。

## 验证

```sh
go vet ./...
go test -race ./...
go run ./cmd/demo
```
