# resourceledger112

并发安全的内存型资源计量账本。原子批次按输入顺序执行 `Add`/`Set`/`Delete`，
失败整体回滚；`Top` 按数值降序、名称升序，`Snapshot` 按名称排序。
仅依赖标准库，Go 1.22+。详细语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- 不维护有序索引：`Top`/`Snapshot` 在读取时全量拷贝后排序，避免写路径
  为排序视图付出额外成本，也天然保证返回切片与内部状态隔离。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind、名称字符集与字节上限），不触碰状态。
- 随后在账户表的浅拷贝（候选事务）上按顺序执行操作：每个 `Add`/`Set`
  分配连续 revision，`Add` 在算术前检测 int64 溢出，每步结果立即执行
  绝对值上限检查，`Delete` 缺失账户即 `ErrNotFound`。
- 账户容量（`MaxAccounts`）仅在批次末对候选结果检查。
- 任一步失败直接丢弃候选，账本、generation、revision 完全不变；
  全部成功才一次性提交并令 generation 恰好 +1（空批次不变）。

**所有权与并发**
- `Ledger` 内部状态（map、generation、nextRev）完全私有，由一把
  `sync.RWMutex` 保护：`Apply` 持写锁，`Top`/`Snapshot` 持读锁。
- 所有返回值（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建切片，
  调用方修改不影响账本；`Account` 为纯值类型，无共享指针。

**复杂度**（n = 账户数，k = 批内操作数）
- `Apply`：时间 O(n + k)（克隆候选表 + 顺序执行），空间 O(n)。
- `Top`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)（按名排序），空间 O(n)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
