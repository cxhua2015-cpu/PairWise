# balanceledger347

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：账户状态存放在唯一的 `map[string]Account` 主索引中，以名称为键。`Top` 与 `Snapshot` 在读取时拷贝条目并分别按（值降序、名称升序）与（名称升序）排序，不维护额外的有序索引，避免写路径的额外开销与一致性问题。
- **候选事务**：`Apply` 先做完整结构校验（kind、名称字符集与长度），再在写锁内把当前账户拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete；任一步失败（`ErrNotFound`/`ErrValue`/`ErrCapacity`）直接丢弃候选，实现整体回滚。int64 溢出在加法前用边界比较检测，绝对值上限对输入与结果同时校验，账户容量仅在批次末对候选检查。全部通过后一次性换入候选，generation 只加一，revision 连续分配。
- **所有权**：`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新分配的副本，调用方修改不影响内部状态；`Apply` 成功后内部整体替换 map，旧 map 不再被账本引用。
- **并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Top`/`Snapshot` 持读锁，所有公开方法可并发调用。
- **复杂度**：`Apply` 为 O(n + a)（n 为批内操作数，a 为当前账户数，候选拷贝）；`Top`/`Snapshot` 为 O(a log a)；空间 O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
