# resourcecatalog111

并发安全的内存型资源目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**：`Store` 以 `map[string]Record` 作为主索引，键即资源名称；另维护
`totalValue`（Value 总字节数）、`generation` 与 `nextRevision` 三个计数器。
单把 `sync.Mutex` 保护全部状态，所有公开方法（`Apply`/`Get`/`Snapshot`）均可并发调用。

**候选事务**：`Apply` 先在锁内对整个批次做完整结构校验（kind、名称字符集与长度、
Value 长度、Delete 不带 Value），不做任何状态读取；随后把当前 map 浅拷贝为候选
（candidate），按输入顺序在其上执行 Put/Delete——Put 克隆 Value 并分配连续
revision，Delete 要求记录存在（否则 `ErrNotFound`）。批次末才检查记录数与 Value
总字节容量（`ErrCapacity`）。任何失败直接丢弃候选，已提交状态、generation 与
revision 完全不变；成功则整体换入候选，generation 只增一次。`Changed` 按名称排序，
记录每个被 Put 名称的最终状态。

**所有权**：Put 时克隆调用方传入的 Value；`Get`/`Snapshot` 返回深拷贝的 Value 与
独立切片，调用方对返回值的任何修改都不会影响内部状态，反之亦然。

**复杂度**：批次含 k 个 op、当前 n 条记录时，`Apply` 为 O(n + k) 时间与 O(n + k)
额外空间（候选拷贝 + Changed 排序 O(k log k)）；`Get` 为 O(1) 加 O(v) 拷贝
（v 为 Value 长度）；`Snapshot` 为 O(n log n)（排序）加 O(Σv) 拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
