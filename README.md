# metacatalog286

并发安全的内存型“元数据目录 286”，仅依赖 Go 标准库（Go 1.22+）。原子批次按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配；完整结构校验先于状态读取；记录数与 Value 总字节容量只在批次末检查；任何失败回滚全部状态、generation 与 revision。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`Store`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义（kind、名称字符集与长度、Value 上限、Delete 不得携带 Value）。
- `stats.go` — 线性一致的 `Stats` 摘要（在读锁内一次性采集）。
- `clone.go` — 保留逻辑时钟（generation / nextRevision）且所有权完全隔离的 `Clone`。

## 索引

主索引为 `map[string]Record` 哈希表，按名称 O(1) 定位；另维护 `totalValue` 运行合计，使容量检查无需遍历。排序（Snapshot、Changed）仅在读取边界按需进行，不写索引。

## 候选事务

`Apply` 先调用 `ValidateBatch` 做纯结构预检（不读状态），再在写锁内把当前记录复制为候选 map，按输入顺序在其上应用 Put/Delete。Put 从候选状态读取旧值并分配连续 revision；Delete 要求候选中存在该名称，否则 `ErrNotFound`。仅在全部操作成功后，才对候选终态检查 `MaxRecords` 与 `MaxTotalValueBytes`（`ErrCapacity`）。任一失败直接丢弃候选，原状态、generation、revision 保持不变；成功时整体换入候选，非空批次 generation 恰好加一。

## 所有权

写入边界的 `Put` Value 立即深拷贝；读取边界（`Get`、`Snapshot`、`Result.Changed`、`Clone`）均返回独立副本，调用方对返回切片的任何修改都不会影响存储，反之亦然。`Clone` 复制全部记录与逻辑时钟，克隆体与原存储不共享任何内存。

## 并发与复杂度

所有公开方法并发安全：写操作（`Apply`）持互斥写锁，读操作（`Get`、`Snapshot`、`Stats`、`Clone`）持读锁可并行。设批次含 b 个操作、存储 n 条记录：

- `Apply`：时间 O(n + b)，空间 O(n)（候选副本）。
- `Get`：O(1)；`Stats`：O(1)。
- `Snapshot` / `Clone`：O(n log n)（排序）/ O(n)。
- `ValidateBatch`：O(b)，无状态访问。

## 测试

`contract_test.go` 与 `integration_test.go` 为契约测试；`boundary_test.go` 补充边界（Options、名称字符集、结构先于状态、容量回滚、空批次、Delete 语义、快照/克隆隔离）与混合并发测试。运行：

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
