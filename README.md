# resourcelease114

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Entry`，键即租约键，按 key O(1) 定位。
- 不维护堆等过期索引：候选事务与 `Expire` 均全量扫描淘汰 `ExpiresAt <= Now` 的条目（闭区间），条目数为 n 时成本 O(n)；在控制面租约规模下足够简单可靠。
- `Snapshot` 与 `Expire` 返回的条目按 key 排序，保证输出确定性。

## 候选事务

`Apply` 分两阶段：

1. **结构校验**（不加锁）：批次与每个 Op 的 kind、键字符集（非空 ASCII 小写字母/数字/`-`/`_`）、键长与 `ExpiresAt` 非负；失败返回 `ErrInvalidInput`，不触碰状态。
2. **候选执行**（持锁）：检查时间单调性（`Now < now` → `ErrTime`），把未过期条目复制到候选 map，先淘汰 `ExpiresAt <= Now`，再顺序执行 Put/Touch/Delete；Put/Touch 从候选 revision 计数器分配递增 revision，Touch/Delete 缺失键 → `ErrNotFound`。最终容量超限 → `ErrCapacity`。

任何错误都直接丢弃候选 map、候选 revision 与候选时间，即淘汰、时间和 revision 一并回滚；只有全部成功才一次性提交。非空成功批次 generation 恰好 +1，空批次不改变任何状态。

## 所有权

- 表内部只持有 `Entry` 值（不含指针/切片），写入候选 map 即完成拷贝。
- `Snapshot` 与 `Expire` 返回新建的切片与 `Entry` 副本，调用方修改不影响内部状态；返回后内部不再引用这些切片。

## 并发与复杂度

- 所有公开方法经单一 `sync.Mutex` 串行化，可安全并发调用；`New` 与结构校验在锁外完成。
- `Apply`：结构校验 O(L)（L 为键总长度），候选复制与淘汰 O(n)，执行 O(m)（m 为 Op 数）。
- `Expire`：O(n)；`Snapshot`：O(n log n)（排序）。
- 空间 O(n)，n 为存活条目数，受 `Options.MaxEntries` 上限约束。
