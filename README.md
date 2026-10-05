# resourcecatalog121

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：记录存储在 `map[string]Record` 中，按名称 O(1) 定位；名称即唯一键。`Snapshot` 与 `Result.Changed` 在返回前对名称排序，保证确定性输出。

**候选事务**：`Apply` 分三个阶段——(1) 完整结构校验（kind、名称字符集与长度、Value 长度），不读取任何状态；(2) 在记录的候选副本（clone 的 map）上按输入顺序执行 Put/Delete，Put 从局部 `nextRevision` 计数器分配连续 revision，Delete 要求目标存在；(3) 批次末检查最终记录数与 Value 总字节容量。任一阶段失败直接返回，候选副本与局部计数器被丢弃，已提交状态、generation 和 revision 完全不变；成功时整体换入候选副本，非空批次 generation 恰好加一。

**所有权**：Put 的 Value 在写入时深拷贝，调用方之后修改入参不影响目录；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方修改返回值不会污染内部状态。返回的切片均为新建，与内部状态隔离。

**并发**：单把 `sync.RWMutex` 保护全部状态；`Apply` 持写锁，`Get`/`Snapshot` 持读锁，可并行读取。

**复杂度**：设批次含 k 个 op、目录含 n 条记录。`Apply` 为 O(n + k + c log c)（候选复制 O(n)，执行 O(k)，结果排序 O(c log c)，c 为变更名称数）；容量检查 O(n)。`Get` 为 O(1) 均摊。`Snapshot` 为 O(n log n)。空间 O(n)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
