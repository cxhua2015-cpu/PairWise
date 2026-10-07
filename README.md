# expirytable309

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引与数据结构

- 主索引为 `map[string]Entry`，键到条目 O(1) 定位；不维护额外的堆/时间轮。
- 到期通过线性扫描完成（`ExpiresAt <= now` 闭区间），单次 O(n)。条目数受 `MaxEntries` 上限约束，扫描成本有界。
- `Snapshot` 与 `Expire` 返回的切片按键排序，且为全新拷贝，与内部状态完全隔离（所有权见下）。

## 候选事务（Apply）

`Apply` 采用候选事务（copy-on-write 候选状态）：

1. **结构校验**：先对整个批次做纯结构校验（kind 合法、键非空且仅含 `[a-z0-9-_]`、键长与 `ExpiresAt` 非负），不读取任何状态，失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now` 必须非负且不早于当前时间，否则 `ErrTime`。
3. **候选状态**：克隆当前条目，先在候选上淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从 `nextRevision` 起分配递增 revision，Touch/Delete 缺失键返回 `ErrNotFound`。
4. **容量裁决**：候选最终大小超过 `MaxEntries` 返回 `ErrCapacity`。
5. **提交或回滚**：任何错误都直接丢弃候选——淘汰、时间与 revision 一并回滚；成功才整体提交。非空批次 `generation` 恰好 +1，空批次不变。

`Expire(now)` 使用相同的闭区间边界与单调时间约束，返回被淘汰条目。

## 所有权与并发

- 所有公开方法由单一互斥锁保护，可安全并发调用；锁内无阻塞调用，临界区短。
- 返回的 `Entry` 切片（`Expire`、`Snapshot`）由 callee 新分配，调用方获得独立所有权，修改不影响表内状态；传入的 `Batch`/`Op` 仅按值读取，不被保留。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（克隆+淘汰扫描），m 为批次操作数；空间 O(n)。
- `Expire`：O(n)；`Snapshot`：O(n log n)（排序）；`New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
