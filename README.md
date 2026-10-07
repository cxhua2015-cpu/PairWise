# balanceledger312

并发安全的内存型“余额账本 312”（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，失败整体回滚。语义细节见 `SPEC.md`。

## 索引

- 主索引：`map[string]Account`，按名称 O(1) 定位账户。
- 无持久化辅助索引：`Top` 与 `Snapshot` 在读取时全量拷贝后排序
  （Top 按值降序、名称升序；Snapshot 按名称升序），避免维护易错的有序结构。

## 候选事务（candidate transaction）

`Apply` 分三个阶段：

1. **结构校验**：在读取任何状态前校验全部 op 的 kind 与名称合法性，
   不合法返回 `ErrInvalidInput`；空批次直接返回，不改变 generation。
2. **候选执行**：克隆当前账户 map 得到候选副本，按输入顺序在其上执行
   Add/Set/Delete。Add/Set 分配连续 revision；算术前检测 int64 溢出并执行
   绝对值上限（`ErrValue`），Delete 缺失账户返回 `ErrNotFound`。
3. **提交**：仅在批次末检查最终账户容量（`ErrCapacity`），通过后整体替换
   内部 map，generation 加一。任一步失败直接丢弃候选副本，内部状态零改动，
   实现整体回滚。

## 所有权

- 所有公开方法由一把 `sync.RWMutex` 保护：`Apply` 持写锁，`Top`/`Snapshot`
  持读锁，可并发调用。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建拷贝，
  调用方修改不会影响内部状态。

## 复杂度

设 n 为账户数、k 为批次内 op 数：

- `Apply`：O(n + k)（克隆候选 map + 顺序执行），回滚无额外成本。
- `Top`：O(n log n) 排序，取前 m 个。
- `Snapshot`：O(n log n) 排序。
- 空间：O(n)，`Apply` 期间临时 O(n) 候选副本。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
