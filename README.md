# expirytable334

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，以键定位条目，Put/Touch/Delete 均为 O(1) 均摊。
- 到期采用惰性策略：`Apply` 在候选状态上先扫描剔除 `ExpiresAt <= Now` 的条目，`Expire` 主动扫描剔除，均为 O(n) 全表扫描（n 为当前条目数），不维护额外堆结构，实现简单且无并发隐患。
- `Snapshot` 返回按字典序排序的条目副本，排序 O(n log n)。

### 候选事务
`Apply` 分四个阶段，任一阶段失败都整体回滚：
1. **结构校验**：整批校验 kind、键字符集（非空 ASCII 小写字母/数字/`-`/`_`，长度 ≤ `MaxKeyBytes`）、非负时间，不读取任何状态，失败返回 `ErrInvalidInput`。
2. **时间检查**：`Now` 必须不小于表当前时间，否则 `ErrTime`。
3. **候选执行**：在条目映射的副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 从候选 revision 计数器分配版本号，Touch/Delete 目标缺失返回 `ErrNotFound`。
4. **提交**：最终容量 ≤ `MaxEntries` 才用候选状态原子替换表状态并推进时间与 revision，否则返回 `ErrCapacity`。因为所有变更都发生在副本上，任何错误天然不泄漏淘汰、时间或 revision。非空成功批次 generation 恰好 +1，空批次不变。

### 所有权
- 表内部状态只由 `Table` 持有，所有公开方法经互斥锁串行化，可并发调用。
- `Snapshot` 与 `Expire` 返回的切片均为新建副本，调用方修改不影响内部状态；`Entry`/`Result`/`Snapshot` 为纯值类型，无共享指针。

### 复杂度
- `Apply`：O(n + m)，n 为条目数（候选复制与淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（结果排序）。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
