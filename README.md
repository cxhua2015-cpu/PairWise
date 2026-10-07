# metacatalog326

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Record`，以名称作为唯一键，Put/Delete/Get 均为 O(1) 均摊查找。
- 另维护 `totalValue`（Value 字节总量）运行计数，避免每次容量检查时全表扫描。
- `Snapshot` 与 `Result.Changed` 在返回前按名称排序，排序代价为 O(n log n)，n 为相关记录数。

**候选事务（candidate transaction）**
- `Apply` 分三个阶段：先对全部 Op 做完整结构校验（不读取任何状态）；再在索引的候选副本上按输入顺序执行 Put/Delete；最后仅在批次末检查记录数与 Value 总字节容量。
- 任一步失败直接丢弃候选副本，已提交状态、generation 与 revision 计数器完全不受影响，天然实现回滚，无需撤销日志。
- Put 在候选上分配连续 revision，Delete 不分配；只有非空成功批次提交时 generation 恰好加一。

**所有权**
- Put 时深拷贝调用方传入的 Value；Get/Snapshot/Result 返回的 Value 与 Record 切片均为新分配的副本。
- 调用方对返回值的任何修改都不会影响目录内部状态，反之亦然。

**并发与复杂度**
- 所有公开方法可并发调用：Apply 持写锁串行提交，Get/Snapshot 持读锁可并行。
- 单次 Apply：结构校验 O(k)，候选执行 O(k)，候选复制 O(n)，批次末容量检查 O(1)；其中 k 为批次内 Op 数，n 为当前记录数。
- Get O(1) 均摊；Snapshot O(n log n)（排序主导）。

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
