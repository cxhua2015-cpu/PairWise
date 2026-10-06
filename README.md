# expirytable234

并发安全的内存型“到期状态表 234”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 架构与索引

- `heartbeat.go`：核心事务引擎。`Table` 以 `map[string]Entry` 作为主索引（按键 O(1) 定位），
  配一把 `sync.Mutex` 保证所有公开方法（`Apply`/`Expire`/`Snapshot`/`Stats`/`Clone`/`ValidateBatch`）
  可并发调用且线性一致。`Snapshot` 返回按字典序排序的条目副本。
- `validation.go`：无副作用的批次结构预检。`ValidateBatch` 与 `Apply` 共享同一个
  `validateBatchShape`：非负 `Now`、合法 kind、键非空且仅含 ASCII 小写字母/数字/连字符/下划线、
  键长不超过 `Options.MaxKeyBytes`、`Put`/`Touch` 要求 `ExpiresAt > Now`。预检不读取、不修改表状态。
- `stats.go`：`Stats` 在同一把锁内读取逻辑时钟与条目数，提供线性一致的状态摘要。
- `clone.go`：`Clone` 在锁内逐条复制 map，保留 generation/nextRevision/now 逻辑时钟，
  副本与原表所有权完全隔离（互不影响）。

## 候选事务

`Apply` 先结构校验、再检查单调时间（`Now < now` 返回 `ErrTime`），随后在**候选副本**上执行：
先淘汰 `ExpiresAt <= Now` 的条目（闭区间），再顺序执行 `Put`/`Touch`/`Delete`；
`Put`/`Touch` 分配单调递增的 revision。任何错误（`ErrNotFound`、最终容量 `ErrCapacity` 等）
都会丢弃候选，淘汰、时间与 revision 一并回滚；只有全部成功才一次性提交。
非空成功批次 generation 恰好加一，空批次不变。`Expire` 使用相同的闭区间边界。

## 所有权

所有返回的切片（`Snapshot.Entries`、`Expire` 结果）都是新分配的拷贝，与内部状态隔离；
`Clone` 的深拷贝不共享任何可写内存。

## 复杂度

- `Apply`：O(E + K)，E 为当前条目数（候选复制与淘汰扫描），K 为批内操作数。
- `Expire`：O(E + R log R)，R 为到期条目数（结果排序）。
- `Snapshot`/`Clone`：O(E log E) / O(E)。`Stats`：O(1)。单操作定位：O(1) 均摊。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
