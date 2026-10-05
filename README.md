# resourceledger177

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- 不维护持久化的排序索引：`Top` 与 `Snapshot` 在读取时现排序（账户数在控制面场景下有限，避免写路径维护堆/树的开销与复杂度）。

**候选事务（两阶段批次）**
- `Apply` 先做整批结构校验（kind 合法、未用字段为零、名称字符集与字节上限），不触碰任何状态。
- 通过后在写锁内把账户表浅拷贝为候选工作副本，按输入顺序在其上执行 Add/Set/Delete：Add/Set 递增分配连续 revision，算术前检测 int64 溢出并执行 `MaxAbsValue` 绝对值上限，Delete 缺失账户返回 `ErrNotFound`。
- 仅在批次末检查最终账户数 `MaxAccounts`；任一失败直接丢弃候选副本，整体回滚，`generation`/`nextRevision` 不变。
- 全部成功才原子替换账户表，`generation` 恰好加一（空批次不变）。

**所有权与并发**
- 单个 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Top`/`Snapshot` 持读锁，可多读者并发。
- 返回的 `[]Account` 均为新建切片并拷贝元素，调用方修改不影响内部状态；`Account` 为纯值类型，无共享指针。

**复杂度**（n = 账户数，b = 批内 op 数）
- `Apply`：时间 O(n + b)，空间 O(n)（候选副本）。
- `Top(k)`：O(n log n)，空间 O(n)。
- `Snapshot`：O(n log n)，空间 O(n)。
- `New`：O(1)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
