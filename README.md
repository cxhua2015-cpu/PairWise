# expirytable264

并发安全的内存型“到期状态表 264”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## Multi-file architecture

实现按职责拆分为四个相互联动的文件，共享同一套结构语义与逻辑时钟：

- `heartbeat.go` — 核心事务引擎：`New` / `Apply` / `Expire` / `Snapshot`，以及表的状态与锁。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 只做结构校验（非负时间、已知 kind、键字符集与字节上限、Put/Touch 的 `ExpiresAt > Now`），不读取也不修改表状态；`Apply` 复用同一套校验，保证预检与事务语义一致。
- `stats.go` — `Stats` 在表锁内一次性读取，返回线性一致的状态摘要。
- `clone.go` — `Clone` 深拷贝全部状态（含 `now`、`generation`、`nextRevision`），克隆体与原表完全隔离。

## 索引与所有权

- 主索引为 `map[string]Entry`，按键 O(1) 定位；`Snapshot`/`Expire` 返回的条目按键排序以保证确定性。
- 所有公开方法返回的切片均为新建副本，与内部状态无共享所有权；`Clone` 逐条复制 map，两个表互不影响。
- 并发安全由单一 `sync.Mutex` 保证：每个公开方法整体持锁，`Stats`/`Snapshot`/`Clone` 因此观察到线性一致的状态点。

## 候选事务与回滚

`Apply` 的顺序为：结构校验（`ValidateBatch`）→ 单调时间检查（`Now < now` 返回 `ErrTime`）→ 在候选 map 上先淘汰 `ExpiresAt <= Now` 的条目 → 顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision）→ 最终容量检查。候选状态只有全部成功才整体提交；任何错误（`ErrNotFound`、`ErrCapacity` 等）都会连同淘汰、时间、generation 和 revision 一起回滚。非空成功批次 generation 恰好加一，空批次不变。`Expire` 使用相同的闭区间边界 `ExpiresAt <= now`。

## 复杂度

- `Apply`：O(n + m)，n 为现有条目数（候选复制与淘汰），m 为批次内 op 数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序）。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `Stats` / `ValidateBatch`：O(1) / O(m·L)，L 为键长。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
