# expirytable324

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 索引

主索引为 `map[string]Entry` 哈希表，以键定位条目，Put/Touch/Delete 均为 O(1) 均摊。
到期淘汰（Apply 候选阶段与 `Expire`）采用全表扫描，O(n)；`Snapshot` 额外按键排序，O(n log n)。
未维护额外的堆/时间轮索引：容量有界（`Options.MaxEntries`），扫描成本可控，且实现更简单、无并发窗口。

## 候选事务

`Apply` 分两阶段执行：

1. **校验**：先对整个批次做完整结构校验（kind、键字符集与字节上限），再检查时间（非负、单调不回退）。任何错误都不读取/修改状态。
2. **候选执行**：克隆当前索引为候选状态，先在候选上删除 `ExpiresAt <= Now` 的条目（闭区间），再按顺序回放 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。回放结束后做**最终容量检查**。

任一环节失败（`ErrNotFound`、`ErrCapacity` 等）直接丢弃候选，淘汰、时间与 revision 一并回滚，表保持调用前状态。全部成功才一次性提交：替换索引、推进 `Now`、提交 revision，且非空批次 `generation` 恰好加一；空批次不做任何修改。

`Expire(now)` 使用相同闭区间边界（`ExpiresAt <= now`），推进时间并返回被淘汰条目（按键排序）。

## 所有权与并发

- 所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；状态变更在锁内原子提交。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不会影响内部状态；`Entry` 为纯值类型，无共享指针。
- 表不持有调用方数据：`Batch`/`Op` 仅在调用期间读取，键字符串按值存入索引。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `Apply`（m 个 op，n 个条目） | O(n + m) | O(n) 候选克隆 |
| `Expire` | O(n + k log k)，k 为淘汰数 | O(k) |
| `Snapshot` | O(n log n) | O(n) |
| `New` / 校验 | O(1) / O(m·keylen) | O(1) |

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
