# expirytable344

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`（键 → 条目），Put/Touch/Delete 与过期扫描均基于它。
- 未维护按到期时间排序的辅助索引：过期扫描为全表遍历，换取实现简单与回滚安全。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证确定性输出。

## 候选事务

`Apply` 采用候选状态（copy-on-write）事务模型：

1. 先对整个批次做结构校验（kind、键字符集/长度、非负时间），再检查单调时间（`Now < now` → `ErrTime`）。
2. 在候选副本上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRevision` 分配 revision。
3. 最终容量超限（`ErrCapacity`）或任何错误发生时直接丢弃候选副本——淘汰、时间与 revision 一并回滚，原状态零改动。
4. 全部成功才提交：替换条目表、推进 `now` 与 `nextRevision`，非空批次 `generation` 恰好加一；空批次不改变任何状态。

`Expire(now)` 使用相同闭区间边界（`ExpiresAt <= now`），移除并返回过期条目；有条目被移除时推进 `now` 并使 `generation` 加一。

## 所有权

- 表内部只持有不可变语义的 `Entry` 值；`Snapshot`/`Expire` 返回的切片均为新建副本，调用方修改不影响内部状态。
- 传入的 `Batch`/`Op` 仅被读取，不被保留或修改。

## 并发与复杂度

- 所有公开方法由单个 `sync.Mutex` 保护，可安全并发调用；结构校验在加锁前完成（只读 `Options`）。
- `Apply`：校验 O(L)（L 为键总字节），候选复制 O(n)，执行 O(k)，整体 O(n + k + L)。
- `Expire`：O(n)。`Snapshot`：O(n log n)（排序）。`New`：O(1)。
- n 为当前条目数，k 为批内操作数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
