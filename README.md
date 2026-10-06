# balanceledger272

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引与存储

账户状态存放在单个 `map[string]Account` 中，按名称 O(1) 定位；`Ledger` 内嵌 `sync.RWMutex`：写路径（`Apply`）取互斥锁，读路径（`Top`/`Snapshot`/`Stats`/`Clone`）取读锁，因此所有公开方法均可并发调用。逻辑时钟（`generation`、`nextRevision`）与账户表一同置于锁内，保证 `Stats`/`Snapshot` 是线性一致的一致视图。

### 候选事务

`Apply` 先调用 `ValidateBatch` 做无副作用的纯结构预检（kind、名称字符集与字节上限、操作数幅度），再复制一份候选账户表，按输入顺序在其上执行 Add/Set/Delete：Add/Set 消耗连续 revision，算术前先检测 int64 溢出再执行 `MaxAbsValue` 绝对值上限；账户容量仅在批次末尾检查。任一步失败直接丢弃候选表，实现整体回滚；成功时一次性换入候选表，非空批次 generation 恰好加一。`Changed` 按首次触碰顺序去重，仅包含批次末仍存在的账户。

### 所有权

`Account` 为纯值类型，候选表、快照、Top 结果与 `Clone` 的账户表均为独立拷贝，返回的切片与内部状态完全隔离；`Clone` 连同逻辑时钟一起深拷贝，克隆体与原账本互不影响。

### 复杂度

- `ValidateBatch`：O(B)，B 为批次操作数，不读状态。
- `Apply`：O(A + B)，A 为当前账户数（候选表复制），B 为操作数。
- `Top`：O(A log A) 排序后取前 n（值降序、名称升序）。
- `Snapshot`：O(A log A)（按名称排序）；`Stats`：O(1)；`Clone`：O(A)。
