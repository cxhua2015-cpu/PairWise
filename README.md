# budgetledger

Read `SPEC.md` and implement the package.

## 实现说明

### 索引
账本以 `map[string]Account` 作为主索引，按账户名 O(1) 定位。`Top` 与 `Snapshot` 不维护预排序结构，而是在读取时把 map 内容拷贝到切片后排序：`Top` 按数值降序、名称升序，`Snapshot` 按名称升序。写路径因此保持 O(1) 均摊，读路径为 O(n log n)。

### 候选事务
`Apply` 先做整批结构校验（kind 合法、名称字符集与长度），不触碰状态；随后在写锁内把当前账户表浅拷贝为候选 map，按输入顺序在其上执行 Add/Set/Delete，Add/Set 分配连续 revision。任何一步失败（溢出、绝对值上限、未找到、容量）直接丢弃候选 map，实现整体回滚；全部成功且最终账户数不超容量时，用候选 map 原子替换正式状态，generation 加一。

### 所有权
所有公开方法返回的切片与 `Account` 值均为新分配的拷贝，调用方修改返回值不会影响内部状态；内部也从不保留调用方传入的切片。`Ledger` 内部状态仅由 `sync.RWMutex` 保护访问：`Apply` 持写锁，`Top`/`Snapshot` 持读锁，可并发调用。

### 复杂度
- `New`：O(1)。
- `Apply`：O(n + m)，n 为批内操作数，m 为当前账户数（候选拷贝）；回滚无额外代价。
- `Top(k)`：O(m log m)，返回前 k 项。
- `Snapshot`：O(m log m)。
- 空间：O(m)，批次执行期间额外 O(m) 候选副本。

### 边界语义
- 算术前先检测 int64 溢出（含 `math.MinInt64` 取负的特例），再施加 `MaxAbsValue` 绝对值上限，违反返回 `ErrValue`。
- 容量上限仅在批次末检查，批内可先删后建（违反返回 `ErrCapacity`）。
- 空批次成功且不增加 generation；非空成功批次 generation 恰好加一。
