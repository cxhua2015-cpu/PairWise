# usageledger

并发安全的内存型用量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 实现说明

### 索引
- 主索引为 `map[string]Account`，按账户名 O(1) 定位。
- `Top` 与 `Snapshot` 不维护辅助有序结构，而是按需对当前账户集合排序：
  `Top` 按值降序、名称升序；`Snapshot` 按名称升序。账户数为 n 时排序复杂度 O(n log n)。
- 返回的切片均为新建副本，与内部状态完全隔离，调用方修改不影响账本。

### 候选事务（candidate transaction）
- `Apply` 分两阶段：先对全部操作做完整结构校验（kind、名称字符集与长度），
  再在持锁状态下把主索引浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete。
- Add/Set 在候选上分配连续 revision；算术前先检测 int64 溢出并校验绝对值上限；
  账户容量上限仅在批次末尾对候选统一检查。
- 任一步失败直接丢弃候选，主状态、revision 计数与 generation 完全不变（整体回滚）；
  全部成功则一次性用候选替换主索引，generation 恰好加一，空批次不加。

### 所有权
- `Ledger` 内部状态（map、generation、nextRevision）由一把 `sync.Mutex` 保护，
  所有公开方法均可并发调用；锁内不调用外部代码，无死锁风险。
- `Account` 为纯值类型，候选拷贝与返回值均按值传递，不存在共享可写内存。

### 复杂度
- `Apply`：O(m·n)（m 为批内操作数，n 为账户数，候选拷贝 O(n)，每操作 O(1)）。
- `Top(k)`：O(n log n) 排序 + O(k) 拷贝。
- `Snapshot`：O(n log n)。
- 空间：O(n)，Apply 期间临时多一份 O(n) 候选。
