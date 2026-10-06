# expirytable219

并发安全的内存型“到期状态表 219”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引与数据结构

- 主索引为 `map[string]Entry`，键到条目 O(1) 定位；不维护额外的堆或有序索引。
- 到期处理采用惰性扫描：`Apply` 在候选状态上先淘汰 `ExpiresAt <= Now` 的条目，`Expire` 直接在全表上按同一闭区间边界扫描删除。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

## 候选事务与回滚

`Apply` 分两阶段：

1. **校验**：先对整个批次做完整结构校验（时间非负、kind 合法、键满足 `[a-z0-9_-]` 且不超过 `MaxKeyBytes`、`ExpiresAt` 非负），再检查时间单调性（`Now` 不得回退，否则 `ErrTime`）。校验期间不读取任何表状态。
2. **候选执行**：在条目副本（候选状态）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。任何错误（`ErrNotFound`、最终 `ErrCapacity`）都会丢弃候选，淘汰、时间与 revision 一并回滚，表保持原状。仅当候选容量 `<= MaxEntries` 时才提交：替换条目、推进 `now` 与 `nextRevision`，且非空成功批次 generation 只增加一次；空批次不改变任何状态。

## 所有权与并发

- 所有公开方法（`New` 除外，其返回前无共享）通过单一 `sync.Mutex` 串行化，可安全并发调用。
- 表独占内部条目；`Snapshot.Entries` 与 `Expire` 返回的切片均为新分配的副本，调用方可自由修改，不影响内部状态。
- 键在写入时校验并不可变地存入条目，返回值不共享内部可变内存。

## 复杂度

设 `n` 为当前条目数、`m` 为批次操作数：

- `Apply`：校验 O(Σ键长)，候选复制与淘汰 O(n)，执行 O(m)，合计 O(n + m)，额外空间 O(n)。
- `Expire`：O(n) 扫描 + O(k log k) 排序（k 为到期条目数）。
- `Snapshot`：O(n log n)（复制并排序）。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
