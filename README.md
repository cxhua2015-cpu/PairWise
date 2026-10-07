# balanceledger342

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
`Add`/`Set`/`Delete`，失败整体回滚；`Top` 按数值降序、名称升序；`Snapshot`
按名称排序。详细语义见 `SPEC.md`。

## 设计说明

**索引**
- 账户主存储为 `map[string]Account`（按名称 O(1) 定位）。
- 不维护有序索引：`Top` 与 `Snapshot` 在读取时按 (value desc, name asc) 或
  name 即时排序，避免写路径维护堆/树的开销与复杂度。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind、名称字符集/长度、多余字段），不触碰状态。
- 随后在账户映射的**副本**上按序执行所有操作：算术前检测 int64 溢出并执行
  `MaxAbsValue` 绝对值上限；`Add`/`Set` 从单调计数器分配连续 revision。
- 仅在批次末尾检查最终账户容量 `MaxAccounts`（中间态允许超限）。
- 任一步失败直接丢弃副本，实现零成本整体回滚；全部成功才一次性提交，
  generation 恰好 +1（空批次不变）。

**所有权与并发**
- 所有公开方法持有 `sync.RWMutex`：写操作互斥，读操作可并发。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的
  副本，与内部状态完全隔离；调用方修改不影响账本，后续批次也不影响已返回结果。

**复杂度**（n = 账户数，b = 批次操作数）
- `Apply`：时间 O(b + n)（副本拷贝），空间 O(n)。
- `Top(k)`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
