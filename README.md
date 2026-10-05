# resourceledger172

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- `Top` 与 `Snapshot` 不维护有序索引，而在读取时复制并排序：
  写入路径（Apply）保持 O(1) 摊销，排序成本只发生在查询端。
  排序比较器：`Top` 按数值降序、名称升序；`Snapshot` 按名称升序。

### 候选事务（candidate transaction）
- `Apply` 分两阶段：先对整个批次做完整结构校验（kind、名称字符集与字节上限），
  不读取任何状态；然后在当前账户表的**浅拷贝**（候选状态）上按输入顺序执行
  Add/Set/Delete。
- Add/Set 在候选状态上分配连续 revision；溢出在算术前检测（含 `math.MinInt64`
  取绝对值的边界），绝对值上限作用于输入与运算结果；账户容量上限仅在批次末
  对候选状态检查一次。
- 任一步失败直接丢弃候选状态并返回错误，账本原有 map、generation、
  nextRevision 完全不变，实现整体回滚；全部成功才用候选状态原子替换正式状态，
  且非空成功批次的 generation 只递增一次（空批次不变）。

### 所有权与并发
- `Ledger` 内部所有可变状态由一把 `sync.Mutex` 保护，所有公开方法
  （`Apply`/`Top`/`Snapshot`）均可并发调用。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建副本，
  调用方修改返回值不会影响账本内部状态；`Account` 为纯值类型，map 替换后
  旧候选即被 GC 回收，无共享可写内存。

### 复杂度
- `Apply`：O(k·n) 最坏（k 为批内 op 数，n 为账户数；候选拷贝 O(n)，每个 op O(1)），
  即 O(n + k)。
- `Top`：O(n log n)（全量排序后取前 n）。
- `Snapshot`：O(n log n)（按名称排序）。
- 空间：O(n)，Apply 额外 O(n) 候选拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
