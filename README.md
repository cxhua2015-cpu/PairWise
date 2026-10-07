# balanceledger337

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 账户主索引为 `map[string]Account`，按名称 O(1) 定位。
- 不维护持久化的有序索引：`Top` 与 `Snapshot` 在调用时把账户复制到切片后排序（值降序/名称升序，或按名称升序），避免写路径为有序结构付出额外代价。

### 候选事务（candidate transaction）
- `Apply` 先做整批结构校验（kind、名称字符集与字节上限），不触碰状态。
- 随后在写锁内把当前账户表浅拷贝为候选 map，按输入顺序在候选上执行 Add/Set/Delete：Add/Set 分配连续 revision，算术前检测 int64 溢出并执行绝对值上限，Delete 缺失即 `ErrNotFound`。
- 最终账户容量仅在批次末检查；任一失败直接丢弃候选，账本状态、generation、revision 完全不变（整体回滚）。
- 全部通过后一次性提交：候选 map 替换旧表，非空批次 generation 恰好加一，nextRevision 前进到已分配区间之后。

### 所有权与并发
- 所有公开方法并发安全：`Apply` 持写锁，`Top`/`Snapshot` 持读锁。
- 账户值按值拷贝进出账本；`Result.Changed`、`Top`、`Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态，内部后续变更也不影响已返回结果。
- `Ledger` 不可复制；通过 `New` 返回的指针共享使用。

### 复杂度
设 n 为当前账户数，b 为批内 op 数，k 为 Top 请求数量：
- `Apply`：时间 O(n + b)（候选拷贝 + 顺序执行），空间 O(n + b)。
- `Top`：时间 O(n log n)，空间 O(n)；结果截断至 k。
- `Snapshot`：时间 O(n log n)，空间 O(n)。
