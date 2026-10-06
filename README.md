# balanceledger212

并发安全的内存型“余额账本 212”。语义见 `SPEC.md`。

## 设计说明

- **索引**：账本以 `map[string]Account` 为主索引，按名称 O(1) 定位账户；`Top` 与 `Snapshot` 在读取时物化并排序副本，不维护额外的有序结构，因此写入路径保持 O(1) 摊销。
- **候选事务**：`Apply` 先在持锁状态下把主索引浅拷贝为候选 map（`Account` 为值类型，拷贝即隔离），所有 Add/Set/Delete 与溢出、绝对值上限检查都在候选上执行；只有批次末尾的最终容量检查通过后才整体替换主索引并提交，失败时直接丢弃候选，实现零成本整体回滚。
- **所有权**：`Ledger` 内部状态仅由 `sync.RWMutex` 保护的方法访问；`Apply` 写路径独占锁，`Top`/`Snapshot` 走读锁。所有返回的切片都是新分配的副本，调用方修改不会影响内部状态。
- **复杂度**：`Apply` 为 O(a + k)，其中 a 为当前账户数（候选拷贝）、k 为批内操作数；`Top` 与 `Snapshot` 为 O(a log a) 排序；空间 O(a)。

## 校验

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
