# balanceledger357

并发安全的内存型余额账本，仅依赖标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- `Top` 与 `Snapshot` 不维护有序索引，读取时快照全量并排序：
  `Top` 按值降序、名称升序；`Snapshot` 按名称升序。
  这避免了写路径上的额外索引维护成本，读多写少且基数受 `MaxAccounts` 约束时足够高效。

**候选事务（candidate transaction）**
- `Apply` 先对整批做完整结构校验（kind、名称字符集与字节上限），不触碰状态。
- 随后在写锁内把当前账户表克隆为候选副本，按输入顺序在副本上执行
  Add/Set/Delete：Add/Set 在候选 revision 计数器上分配连续 revision，
  算术前检测 int64 溢出并执行绝对值上限；批次末检查最终账户容量。
- 任一步失败即丢弃候选副本，账本、revision、generation 完全不变（整体回滚）；
  全部成功才一次性提交（换入副本、推进 revision、generation +1）。

**所有权**
- 所有公开方法通过 `sync.RWMutex` 保护，可并发调用；写操作互斥，读操作共享。
- `Result.Changed`、`Top`、`Snapshot` 返回的切片均为新分配的副本，
  调用方修改返回值不会影响账本内部状态，反之亦然。

**复杂度**（n = 账户数，b = 批次内 op 数）
- `Apply`：时间 O(n + b)（克隆 + 顺序执行），空间 O(n)。
- `Top(k)`：时间 O(n log n)，空间 O(n)。
- `Snapshot`：时间 O(n log n)，空间 O(n)。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
