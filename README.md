# resourcecatalog156

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- `records map[string]Record`：名称到记录的主索引，O(1) 定位。
- `revIndex map[uint64]string`：revision 到名称的反向索引，随 Put/Delete 同步维护，保证 revision 全局唯一且连续分配（Delete 不分配）。
- `totalVal`：Value 总字节数的运行计数，避免批次末容量检查时再全表求和。

**候选事务**
- `Apply` 分三个阶段：先对整个批次做纯结构校验（不读任何状态），再按输入顺序在真实状态上执行并记录 undo 日志，最后在批次末统一检查记录数与 Value 总字节容量。
- 任一步失败（`ErrNotFound` / `ErrCapacity`）时按逆序回放 undo 日志，并恢复 `nextRev`，从而回滚全部状态、generation 与 revision；成功的非空批次 generation 只增加一次，空批次不变。

**所有权**
- Put 时拷贝调用方传入的 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方对返回切片的修改不会泄漏回内部状态，反之亦然。
- 所有公开方法由同一把 `sync.Mutex` 保护，可并发调用；返回的切片与内部状态完全隔离。

**复杂度**
- 结构校验 O(B)，B 为批次操作数；执行阶段每个操作 O(1)（map 存取 + Value 拷贝 O(len(Value))）。
- 容量检查 O(1)（维护中的计数器）；回滚 O(B)。
- `Get` O(len(Value))（深拷贝）；`Snapshot` 与 `Result.Changed` 为 O(R log R)（按名称排序），R 为记录数。
