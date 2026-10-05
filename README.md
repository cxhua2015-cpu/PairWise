# resourceledger097

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**：账户状态存放在 `map[string]Account` 这一唯一哈希索引中，按名称 O(1) 定位。`Top`/`Snapshot` 不维护有序索引，而是读取时现排序——写路径保持 O(1)，读路径付出排序成本，避免每次写入都维护堆或平衡树。

**候选事务**：`Apply` 先做完整结构校验（kind、名称字符集与字节上限），不触碰状态；随后在互斥锁内把当前 map 克隆为候选副本，按输入顺序在副本上执行 Add/Set/Delete。Add 在算术前用边界比较检测 int64 溢出，Add/Set 的结果值执行绝对值上限（`MaxAbsValue`），Delete 对缺失账户返回 `ErrNotFound`。账户容量仅在批次末对候选副本检查。任一步失败直接丢弃候选副本，原状态、generation、revision 计数器完全不变，实现整体回滚；全部成功才一次性交换提交，generation 只增一次，revision 由 Add/Set 连续分配。

**所有权**：`Ledger` 内部 map 是唯一的权威状态，绝不外泄。`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新分配的拷贝，调用方修改返回值不影响账本；入参 `Batch` 只读。所有公开方法共用一把 `sync.Mutex`，批次整体串行化，支持任意并发调用。

**复杂度**（n = 账户数，k = 批次数）：
- `New`：O(1)
- `Apply`：O(n + k) 时间与空间（克隆候选副本 + 逐 op O(1)）
- `Top`：O(n log n) 排序后取前 m 个
- `Snapshot`：O(n log n) 按名称排序

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
