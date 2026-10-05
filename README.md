# resourcelease154

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即租约名，Put/Touch/Delete 与过期扫描均为哈希定位。
- 无额外堆/树索引：`Apply` 与 `Expire` 的过期淘汰是对候选 map 的一次线性扫描（`ExpiresAt <= Now`，闭区间）。
- `Snapshot` 与 `Expire` 返回的切片按 Key 字典序排序（规范顺序），且为独立拷贝，与内部状态隔离。

### 候选事务
- `Apply` 先对整批做完整结构校验（kind、键字符集/长度、非负时间），再检查单调时间（`Now >= now`，否则 `ErrTime`）。
- 校验通过后，在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个递增 revision。
- 任一 op 失败（`ErrNotFound`）或最终条目数超过 `MaxEntries`（`ErrCapacity`）时整体回滚：淘汰、时间、revision、generation 均不变。
- 非空成功批次 generation 恰好加一；空批次不改变任何状态。

### 所有权
- `Table` 内部状态（map、时间、计数器）不对外暴露；`Snapshot.Entries` 与 `Expire` 返回值均为新建切片，调用方可自由修改。
- 所有公开方法通过单个 `sync.Mutex` 串行化，支持任意并发调用；批次内操作原子可见。

### 复杂度
- `New`：O(1)。
- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰扫描），m 为批内 op 数。
- `Expire`：O(n + k log k)，k 为过期条目数（排序）。
- `Snapshot`：O(n log n)（排序）；空间 O(n)。
