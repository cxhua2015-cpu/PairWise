# quotaaccount

并发安全的内存型配额账户簿（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`，按名称 O(1) 定位账户。
- `Top` 与 `Snapshot` 不维护有序索引，而是每次调用时对当前账户快照排序：
  `Top` 按数值降序、名称升序；`Snapshot` 按名称升序。账户数在控制面场景下
  规模有限，用排序换取实现的简单与无额外写路径开销。

**候选事务（candidate transaction）**
- `Apply` 先在无锁状态下做完整结构校验（kind、名称字符集与字节上限），
  未知 kind 或非法名称直接返回 `ErrInvalidInput`，不触碰状态。
- 随后在写锁内把账户表克隆为候选副本，按输入顺序在副本上执行
  Add/Set/Delete：Add/Set 在算术前检测 int64 溢出并执行绝对值上限
  （`ErrValue`），Delete 要求账户存在（`ErrNotFound`）；Add/Set 分配连续
  revision。账户容量上限仅在批次末对候选副本检查（`ErrCapacity`）。
- 任一步失败即丢弃候选副本，状态、generation、revision 完全不变（整体回滚）；
  全部成功才一次性提交副本，非空批次 generation 恰好加一，空批次不变。

**所有权**
- 所有公开方法通过 `sync.RWMutex` 保护：`Apply` 持写锁，`Top`/`Snapshot` 持读锁。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建副本，
  与内部 map 中的 `Account` 值类型无共享引用，调用方可自由修改。

**复杂度**
- `Apply`：O(B·A) 克隆（A 为账户数）+ O(B) 执行，B 为批内操作数；空间 O(A)。
- `Top`：O(A log A) 排序，返回前 n 个；`Snapshot`：O(A log A)。
- `New`：O(1)。Options 中容量与长度上限必须为正，否则 `ErrInvalidOptions`。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
