# expirytable309

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（键 → 条目），Put/Touch/Delete 与过期扫描均直接作用于该映射。
- 未维护额外的堆或时间轮：`Apply`/`Expire` 的淘汰是对候选映射的一次线性扫描，按 `ExpiresAt <= Now` 闭区间删除。
- `Snapshot` 与 `Expire` 的返回切片在锁内拷贝后按键（及到期时间）排序，保证输出确定性。

## 候选事务

`Apply` 分两阶段：

1. **校验**：先对整个批次做结构校验（kind、键字符集与长度、非负时间），再检查时间单调性（`Now < 当前时间` 返回 `ErrTime`）。任一失败均不触碰状态。
2. **候选执行**：把当前映射复制为候选副本，先在候选上淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 在候选 revision 计数器上分配新 revision。最后做容量检查（`len > MaxEntries` 返回 `ErrCapacity`）。
3. **提交或回滚**：全部成功才用候选副本原子替换内部映射，并一并提交 `now`、`rev`、`gen+1`；任何错误（`ErrNotFound`、`ErrCapacity` 等）直接丢弃候选，淘汰、时间与 revision 随候选一起回滚。空批次在通过校验与时间检查后是完全无操作，generation 不变；非空成功批次 generation 恰好加一。

`Expire(now)` 使用相同的闭区间边界原地淘汰，推进 `now`，并在确有条目被移除时将 generation 加一。

## 所有权

- 所有公开方法（`New` 除外）通过单个 `sync.Mutex` 串行化，支持任意并发调用。
- `Snapshot().Entries` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；内部也绝不保留调用方传入的切片。
- 表的时间只能前进：`Apply`/`Expire` 携带的 `now` 小于当前时间返回 `ErrTime`，负时间返回 `ErrInvalidInput`。

## 复杂度

设批次含 `b` 个操作、表内 `n` 个条目：

- `Apply`：校验 O(b)，候选复制 O(n)，执行 O(b)，总 O(n + b) 时间、O(n) 额外空间。
- `Expire`：O(n) 扫描 + O(k log k) 排序（k 为淘汰条数）。
- `Snapshot`：O(n log n)（拷贝 + 按键排序）。
- 单操作均摊 O(1) 的键查找由哈希映射保证。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
