# resourcelease194

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键即租约键，查找 / 插入 / 删除均为 O(1) 均摊。
- 不维护堆或有序索引：过期淘汰采用全表扫描（`ExpiresAt <= Now`，闭区间），
  因此单次 `Apply` / `Expire` 为 O(n)，n 为当前条目数。
- `Snapshot` 与 `Expire` 返回的条目按键排序，保证输出确定性。

## 候选事务

`Apply` 分五个阶段，任一阶段失败都整体回滚：

1. **结构校验**：先完整校验整个批次（kind 合法、键为非空 ASCII 小写字母 /
   数字 / `-` / `_` 且不超过 `MaxKeyBytes`、`ExpiresAt` 非负），再读取任何状态。
2. **时间检查**：`Now` 必须非负且不早于当前表时间，否则返回 `ErrTime`。
3. **候选淘汰**：在克隆出的候选 map 上先删除 `ExpiresAt <= Now` 的条目。
4. **顺序执行**：依次执行 Put / Touch / Delete；Put 与 Touch 各分配一个单调
   递增的 revision，Touch / Delete 目标不存在返回 `ErrNotFound`。
5. **最终容量**：候选条目数超过 `MaxEntries` 返回 `ErrCapacity`。

由于所有变更都发生在候选副本上，只有全部成功才一次性提交，因此淘汰、
时间与 revision 随任何错误自动回滚，无需补偿日志。非空成功批次 generation
恰好加一；空批次不改变 generation（仍推进时间并执行淘汰）。

## 所有权

- 所有公开方法持有同一把互斥锁，可安全并发调用。
- `Snapshot.Entries` 与 `Expire` 的返回切片都是新分配的副本，
  调用方修改不影响表内状态；`Entry` 为纯值类型，无共享指针。
- 表不保留调用方传入的切片或字符串以外的引用，输入批次可在返回后复用。

## 复杂度

| 操作 | 时间 | 额外空间 |
| --- | --- | --- |
| `New` | O(1) | O(1) |
| `Apply`（m 个 op，n 个条目） | O(n + m) | O(n) 候选副本 |
| `Expire` | O(n + k log k)（k 为淘汰数） | O(k) |
| `Snapshot` | O(n log n)（排序） | O(n) |

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
