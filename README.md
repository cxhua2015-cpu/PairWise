# expirytable364

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即条目键，Put/Touch/Delete 均为 O(1) 均摊。
- 淘汰（`ExpiresAt <= Now`，闭区间）采用全表扫描，O(n)；条目数量受 `Options.MaxEntries` 上限约束，扫描成本有界。
- `Snapshot` 与 `Expire` 返回的切片按键排序，保证输出确定性。

### 候选事务
- `Apply` 先对整批做完整结构校验（kind、键字符集与字节上限、非负时间），再检查单调时间（`Now` 不得回退，否则 `ErrTime`）。
- 校验通过后，在候选状态（当前条目的副本）上先淘汰 `ExpiresAt <= Now` 的条目，再按顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
- 任一操作失败（`ErrNotFound`）或最终容量超限（`ErrCapacity`）时直接丢弃候选状态：淘汰、时间与 revision 分配全部随错误回滚，已提交状态不受影响。
- 全部成功才一次性提交：替换条目集、推进 `now`、更新 `nextRevision`，非空批次 `generation` 恰好加一；空批次不改变任何状态。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；`Apply`/`Expire` 串行化，单调时间检查在锁内完成。
- `Snapshot` 返回的 `Entries` 与 `Expire` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；`Entry`/`Result`/`Snapshot` 均按值传递，无共享指针。

### 复杂度
- Put/Touch/Delete：均摊 O(1)；含淘汰与候选拷贝的 `Apply` 为 O(n + m)（n 为条目数，m 为批内操作数）。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
