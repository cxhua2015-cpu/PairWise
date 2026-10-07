# balanceledger332

并发安全的内存型“余额账本 332”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`（按名称 O(1) 定位），账户名即唯一键。
- 不维护持久化的有序索引：`Top` 与 `Snapshot` 在读取时按需排序。
  数据规模为控制面级别，读时排序换取写入路径的极简与无额外一致性问题。

### 候选事务（candidate transaction）
- `Apply` 先对整个批次做完整结构校验（kind、名称字符集与长度），不触碰状态。
- 随后在**当前状态的副本**（候选事务）上按输入顺序执行 Add/Set/Delete；
  任何一步失败（溢出/绝对值上限 `ErrValue`、Delete 缺失 `ErrNotFound`、
  批次末容量 `ErrCapacity`）直接丢弃副本，实现整体回滚，无副作用。
- 仅在全部成功且批次末容量检查通过后，用候选 map 原子替换主索引，
  并将 `generation` 加一、推进 `nextRevision`。空批次不增加 generation。

### 所有权与并发
- `Ledger` 内部状态由一把 `sync.RWMutex` 保护：`Apply` 取写锁，
  `Top`/`Snapshot` 取读锁，所有公开方法可并发调用。
- 返回值（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建切片，
  与内部状态完全隔离；调用方修改返回值不影响账本。
- revision 由账本在提交时分配，Add/Set 连续递增；失败批次不消耗
  revision 与 generation。

### 复杂度
- `Apply`：O(k·n) 最坏（k 为批内 op 数，n 为账户数；候选副本 O(n)，
  每个 op O(1)），校验 O(k)。
- `Top`：O(n log n)（全量排序后取前 m）。
- `Snapshot`：O(n log n)（按名称排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
