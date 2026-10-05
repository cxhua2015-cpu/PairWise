# resourceledger167

并发安全的内存型资源计量账本。仅依赖标准库，需 Go 1.22+。语义详见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Account`，按名称 O(1) 定位账户。
- `Top` 与 `Snapshot` 不维护额外有序索引：每次调用时把 map 快照为切片后排序
  （Top 按值降序、名称升序；Snapshot 按名称升序）。账户数受 `MaxAccounts` 上限约束，
  排序开销有界，换来写入路径 O(1) 与实现的简单性。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind 合法、名称字符集与字节上限），不读取任何状态。
- 随后在互斥锁内把当前账户表克隆为候选 map，按输入顺序在其上执行 Add/Set/Delete；
  Add/Set 各分配一个连续 revision。任何一步失败（溢出/绝对值上限 → `ErrValue`，
  删除不存在 → `ErrNotFound`，批次末账户数超限 → `ErrCapacity`）直接丢弃候选 map，
  原状态零改动，实现整体回滚。
- int64 溢出在做加法之前用边界比较检测；`|MinInt64|` 不可表示，故该值被绝对值上限拒绝。
- 全部成功才一次性提交：替换账户表、generation 增一（空批次不增）、推进 revision 计数。

**所有权与并发**
- 所有公开方法可并发调用：写路径持 `sync.Mutex` 写锁，`Top`/`Snapshot` 持 `RLock`。
- 返回值（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建切片 + 值类型 `Account`
  拷贝，与内部状态完全隔离；调用方修改返回数据不影响账本。
- `Ledger` 内部状态不外泄，无指针/切片别名共享。

**复杂度**（n = 账户数，b = 批次内 op 数）
- `Apply`：克隆 O(n) + 执行 O(b)，空间 O(n)。
- `Top`：O(n log n) 时间，O(n) 空间。
- `Snapshot`：O(n log n) 时间，O(n) 空间。
- `New`：O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
