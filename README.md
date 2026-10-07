# expirytable314

并发安全的内存型“到期状态表”，使用显式非负单调时间，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引与数据结构

- 主索引为 `map[string]Entry`，按键 O(1) 定位，条目内联存储 `ExpiresAt` 与 `Revision`。
- 未维护额外的堆/时间轮：到期扫描为全量遍历（见复杂度）。表规模受 `Options.MaxEntries` 约束，遍历成本有界。
- `Snapshot` 与 `Expire` 返回的切片按键排序，保证输出确定性。

## 候选事务（Apply）

`Apply` 采用“候选状态 + 提交”的事务模型：

1. 先对整个批次做完整结构校验（kind、键字符集与字节上限、非负时间），再检查时间单调性，校验期间不读取、不修改任何状态。
2. 在候选副本上先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
3. 任何错误（`ErrNotFound`、`ErrCapacity` 等）直接丢弃候选副本：淘汰、时间与 revision 一并回滚，已发布状态保持不变。
4. 全部成功才一次性提交；非空批次 generation 恰好加一，空批次不变。

`Expire(now)` 使用相同的闭区间边界（`ExpiresAt <= now`），推进表时间并返回被移除的条目。

## 所有权与并发

- 所有公开方法由单个 `sync.Mutex` 保护，可任意并发调用。
- `Snapshot().Entries` 与 `Expire` 的返回值均为新分配的切片与条目副本，调用方修改不会影响内部状态；`Entry` 为纯值类型，无共享指针。
- 内部 map 在每次成功 `Apply` 时整体替换，旧 map 不再被表引用，杜绝写后共享。

## 复杂度

设批次大小为 B、当前条目数为 N：

- `Apply`：校验 O(B)，候选复制 O(N)，执行 O(B)，总计 O(N + B)。
- `Expire`：O(N + E log E)，E 为到期条目数（排序返回）。
- `Snapshot`：O(N log N)（复制并排序）。
- 空间：O(N)，另加 Apply 期间的候选副本 O(N + B)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
