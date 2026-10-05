# metacatalog201

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，按名称 O(1) 定位记录。
- 另维护两个派生计数器：当前 Value 总字节数 `totalValue` 与下一个可分配 revision `nextRevision`，避免每次容量检查都全表扫描。
- 有序视图（`Snapshot`、`Result.Changed`）不做有序索引，而是在读取时收集后按名称排序，写入路径保持 O(1)。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind、名称字符集与长度、Value 长度），失败立即返回 `ErrInvalidInput`，不读取任何状态。
- 校验通过后，把记录表浅拷贝为候选 map，按输入顺序在候选上执行 Put/Delete：Put 分配连续 revision，Delete 不分配；Delete 缺失记录返回 `ErrNotFound`。
- 记录数与 Value 总字节容量只在批次末对候选结果检查，超限返回 `ErrCapacity`。
- 任一步失败直接丢弃候选 map，计数器与 revision 未曾提交，天然完成回滚；全部成功才一次性替换内部状态，非空批次 generation 恰好加一。

**所有权**
- 存入的 Value 在 Put 时拷贝；`Get`、`Snapshot`、`Result.Changed` 返回的 Value 均为深拷贝，调用方对返回切片的修改不影响内部状态，反之亦然。
- 返回的切片（`Records`、`Changed`）每次调用新建，与内部状态完全隔离。

**并发**
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁。所有公开方法可并发调用，已通过 `go test -race` 验证。

**复杂度**
- `Apply`：O(B + N)，B 为批次 op 数，N 为当前记录数（候选拷贝）；Changed 排序 O(C log C)，C 为涉及的不同名称数。
- `Get`：O(1)（不计返回值拷贝的 O(V)）。
- `Snapshot`：O(N log N)。
- 空间：O(N + 总 Value 字节数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
