# balanceledger242

并发安全的内存型余额账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

实现按职责拆分为四个联动文件，共享同一套结构语义：

- `creditpool.go` — 核心事务引擎：`New` / `Apply` / `Top` / `Snapshot`。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 只做结构校验
  （kind、名称字符集与字节上限、未使用字段必须为零、Set 绝对值上限），
  不读取账户状态；`Apply` 在开启候选事务前调用同一条校验路径。
- `stats.go` — `Stats` 在读锁下返回线性一致的 `Generation` / `NextRevision` /
  账户数三元组。
- `clone.go` — `Clone` 在读锁下复制全部账户与逻辑时钟，返回完全独立的账本。

## 索引

账户主索引为 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot`
不维护辅助有序结构，而是在读锁下取出快照切片后排序（分别为值降序/名称升序、
纯名称升序），以换取写入路径的极简与无锁序反转风险。

## 候选事务

`Apply` 先通过 `ValidateBatch` 做完整结构预检，再在写锁内把当前账户表
浅拷贝为**候选事务**：所有 Add/Set/Delete 按输入顺序作用于候选副本，
Add/Set 从 `nextRevision` 起分配连续 revision。int64 溢出在算术前用
边界比较检测，绝对值上限在每次写入后立即检查，账户容量上限仅在批次末
对最终候选表检查。任一步失败直接丢弃候选副本——原状态、generation 与
revision 时钟完全不变，实现整体回滚；全部成功才一次性提交（替换 map、
推进时钟、generation 恰好 +1）。空批次不推进 generation。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）
均为新分配的副本，调用方修改不会影响内部状态。`Clone` 深拷贝账户 map，
克隆体与原账本不共享任何可写内存，逻辑时钟（generation/nextRevision）
随拷贝保留但此后各自独立推进。

## 并发

单把 `sync.RWMutex` 保护全部状态：写路径（`Apply`）持写锁，读路径
（`Top`/`Snapshot`/`Stats`/`Clone`）持读锁，可并发执行。`ValidateBatch`
只读不可变的 `Options`，无需加锁。

## 复杂度

- `Apply`：O(k·n) 候选拷贝 + O(k) 执行，k 为批内 op 数、n 为当前账户数。
- `Top`：O(n log n)；`Snapshot`：O(n log n)；`Stats`：O(1)。
- `Clone`：O(n)；`ValidateBatch`：O(k·L)，L 为名称字节长度。
