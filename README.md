# metacatalog431

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行 Put/Delete；Put 分配连续 revision，Delete 不分配；完整结构校验先于状态读取；记录数与 Value 总字节容量只在批次末检查；失败回滚全部状态、generation 与 revision。

## 文件结构

- `servicecatalog.go` — 核心事务引擎：`Store`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次结构预检（`ValidateBatch`），与 `Apply` 共享同一套结构语义。
- `stats.go` — 线性一致的 `Stats`（读锁下聚合）。
- `clone.go` — 保留逻辑时钟（generation / nextRevision）且所有权完全隔离的深拷贝。
- `preview.go` — `Preview`：在一次线性化快照上复用完整事务语义做候选提交。

## 索引

记录存储在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；`Snapshot`/`Result.Changed` 在返回前按名称排序（O(n log n)）。无额外二级索引。

## 候选事务

`Apply` 在写锁内先复制记录映射，在副本上按序执行全部操作并分配 revision，批次末统一检查 `MaxRecords` 与 `MaxTotalValueBytes`；任一失败直接丢弃副本，原状态、generation、revision 不变（天然回滚）。`Preview` 通过 `Clone` 取得一致快照，在候选 Store 上执行同一事务路径，返回候选 `Result`、`Snapshot`、`Stats`；错误及优先级与同状态 `Apply` 完全一致，失败时全部返回零值，原对象与逻辑时钟不受影响。

## 所有权

所有进入（Put 的 Value）与离开（`Get`、`Snapshot`、`Result.Changed`、`Clone`、`Preview`）的切片均深拷贝，调用方与 Store、原对象与克隆/候选状态之间不共享任何可变内存。返回值可安全修改。

## 并发与复杂度

单把 `sync.RWMutex` 保护全部状态：写操作（`Apply`）取写锁，读操作（`Get`、`Snapshot`、`Stats`、`Clone`、`Preview`）取读锁，可并发执行。设批次长度为 k、记录数为 n：

- `ValidateBatch`：O(k)，不读状态。
- `Apply`：O(n + k)，复制映射 O(n)，执行 O(k)，容量汇总 O(n)。
- `Get`：O(1)（加深拷贝 O(|value|)）。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `Stats`：O(n)；`Preview`：O(n + k) 加一次 O(n) 克隆。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
