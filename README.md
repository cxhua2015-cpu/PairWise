# resourceledger152

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引结构

- 主索引为 `map[string]Account`（名称 → 账户），按名称 O(1) 定位。
- 不维护有序索引：`Top` 在调用时对账户快照按「数值降序、名称升序」排序后截取；`Snapshot` 按名称升序排序。账户数受 `MaxAccounts` 上限约束，按需排序比维护堆/树更简单且足够快。
- 计数器 `generation`（成功非空批次 +1）与 `nextRevision`（每个 Add/Set 分配一个连续 revision）随账本单调递增。

## 候选事务（staging）

`Apply` 分两阶段：

1. **结构校验**：在读取任何状态前，完整校验全部 op 的 kind 与名称（非空、`[a-z0-9-_]`、≤ `MaxNameBytes`），失败返回 `ErrInvalidInput`。
2. **暂存执行**：把当前账户表复制到候选 map，按输入顺序执行 Add/Set/Delete。算术前先检测 int64 溢出（含 `MinInt64` 取绝对值、加法溢出）并执行 `MaxAbsValue` 绝对值上限（`ErrValue`）；Delete 缺失账户返回 `ErrNotFound`。批次末尾才检查最终账户容量（`ErrCapacity`）。任一步失败直接丢弃候选 map，原状态零改动，实现整体回滚；全部成功才一次性提交并递增 generation。

## 所有权与并发

- 所有公开方法由一把 `sync.RWMutex` 保护：`Apply` 取写锁，`Top`/`Snapshot` 取读锁，可多读者并发。
- 返回值（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新分配的切片与值拷贝，调用方修改不会影响内部状态，后续批次也不会改变已返回的数据。

## 复杂度

设 n = 账户数（≤ MaxAccounts），k = 批次内 op 数：

- `Apply`：O(n + k)（复制候选表 O(n)，顺序执行 O(k)）。
- `Top`：O(n log n) 排序 + O(n) 拷贝。
- `Snapshot`：O(n log n) 排序 + O(n) 拷贝。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
