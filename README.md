# resourcecatalog156

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位；`Record` 内联保存 `Value` 与 `Revision`。
- 另维护 `totalValue` 计数器，避免每次容量检查时遍历求和。
- `Snapshot` 与 `Result.Changed` 按需对名称排序输出，索引本身无序。

**候选事务（candidate transaction）**
- `Apply` 分三个阶段：先对整个批次做完整结构校验（不读任何状态）；再在写锁内把当前索引浅拷贝为候选 map，按输入顺序应用 Put/Delete（Put 分配连续 revision，Delete 不分配）；最后仅在批次末检查记录数与 Value 总字节容量。
- 任何失败（`ErrNotFound` / `ErrCapacity`）直接丢弃候选 map，`generation`、`revision` 与记录状态全部保持不变，天然回滚；成功时一次性换入候选 map，`generation` 只加一。

**所有权**
- Put 的 `Value` 在入库时深拷贝；`Get`、`Snapshot`、`Result.Changed` 返回的 `Value` 与记录切片均为独立副本，调用方修改返回值或后续修改入参都不会影响内部状态。

**并发**
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可并发调用（`-race` 通过）。

**复杂度**
- `Apply`：O(B + N + C log C)，B 为批内操作数，N 为当前记录数（候选拷贝），C 为批内变更的不同名称数（排序）。
- `Get`：O(1)（加值拷贝 O(V)）。
- `Snapshot`：O(N log N)（排序）+ O(总字节数)（深拷贝）。
- 空间：O(N + 总 Value 字节)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
