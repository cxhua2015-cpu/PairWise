# expirytable439

并发安全的内存型“到期状态表 439”（Go 1.22+，仅标准库）。表使用显式非负单调逻辑时间，所有公开方法均可并发调用。

## 架构与索引

- `heartbeat.go`：核心事务引擎。`Table` 持有 `sync.RWMutex`、逻辑时钟（`now`）、`generation`、单调递增的 `nextRevision`，以及**条目切片 + `map[string]int` 索引**的双重表示：切片保持插入序用于 `Snapshot`/`Expire` 的确定性输出，哈希索引使按键定位均摊 O(1)。
- `validation.go`：`ValidateBatch` 只做无副作用的结构预检（kind 合法、键为非空 `[a-z0-9_-]` 且不超 `MaxKeyBytes`、`ExpiresAt` 非负、Put/Touch 的 `ExpiresAt > Now`），不读取任何状态；`Apply` 复用同一函数，保证结构语义一致。
- `stats.go`：`Stats` 在读锁下一次性读取，返回线性一致的状态摘要。
- `clone.go`：`Clone` 在读锁下深拷贝全部字段（含逻辑时钟与 revision 计数），新表持有独立的 map、切片与互斥锁，所有权完全隔离。
- `preview.go`：`Preview` 先 `Clone` 取得一次线性化快照，再在克隆上执行完整的 `Apply` 事务，返回候选 `Result`/`Snapshot`/`Stats`；原对象的状态、generation、revision 与逻辑时间均不变，错误及优先级（结构校验 → 时间 → NotFound → 容量）与同状态上的 `Apply` 完全一致，失败时所有返回值为零值。

## 候选事务与回滚

`Apply` 先结构校验、再检查时间（`Now` 非负且不后退），然后在**候选状态**（索引与条目切片的副本）上执行：先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 Put/Touch/Delete；Put/Touch 各分配一个 revision。最终条目数超过 `MaxEntries` 或任一操作失败（如 Touch/Delete 不存在的键）时整体回滚——淘汰、时间与 revision 分配全部作废。非空成功批次 `generation` 恰好加一，空批次不变（但仍推进时间）。`Expire` 使用相同的闭区间边界并推进逻辑时钟。

## 所有权与隔离

所有返回的切片（`Snapshot.Entries`、`Expire` 结果）都是内部状态的副本；`Clone`/`Preview` 的产物与原对象无任何共享内存，修改互不可见。

## 复杂度

设批次含 `k` 个操作、表内 `n` 个条目：

- `Apply` / `Preview`：O(n + k)（候选复制 + 顺序执行），`Preview` 额外一次 O(n) 克隆。
- `Expire` / `Snapshot` / `Clone`：O(n)。
- `Stats` / `ValidateBatch`：O(1) / O(k)。
- 单键定位均摊 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
