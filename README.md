# balanceledger427

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 架构与索引

- `creditpool.go`：核心事务引擎。状态为 `map[string]Account` 哈希索引（按名 O(1) 定位），
  配一把 `sync.RWMutex`：写路径（`Apply`）独占，读路径（`Top`/`Snapshot`/`Stats`/`Clone`/`Preview`）共享。
  逻辑时钟为 `generation`（每个非空成功批次 +1）与 `nextRevision`（每个 Add/Set 单调分配）。
- `validation.go`：无副作用的结构预检（名称字符集/长度、kind 合法性、字段纪律），
  不读取任何状态；`Apply` 与 `ValidateBatch` 共享同一 `validateBatch`，保证语义一致。
- `stats.go`：在读锁内一次性生成线性一致的 `Stats`。
- `clone.go`：深拷贝全部账户与逻辑时钟，新对象持有独立 map 与互斥锁，所有权完全隔离。
- `preview.go`：候选事务。读锁内克隆一致快照后立即释放，再在候选副本上执行完整 `Apply`
  语义，返回候选 `Result`/`Snapshot`/`Stats`；原对象状态、时钟与所有权均不变，
  错误及优先级与同状态 `Apply` 完全一致，失败时全部返回零值。

## 事务语义

批次先完整结构校验，再按输入顺序执行；Add/Set 在算术前检测 int64 溢出并执行绝对值
上限（`ErrValue`），最终账户容量仅在批次末检查（`ErrCapacity`）。任何失败通过逐账户
备份整体回滚，时钟不变。返回的切片均为新建拷贝，与内部状态隔离。

## 复杂度

- `Apply`：O(k)，k 为批内 op 数（回滚同为 O(k)）。
- `ValidateBatch`：O(k)，零分配状态读取。
- `Top`：O(n log n)，n 为账户数；`Snapshot`：O(n log n)（按名排序）。
- `Stats`：O(1)；`Clone`/`Preview`：O(n) 拷贝 + 候选批次 O(k)。

## 验证

`go test ./...`、`go test -race ./...`、`go run ./cmd/demo`。
