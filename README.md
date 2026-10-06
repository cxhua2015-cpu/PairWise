# balanceledger262

并发安全的内存型余额账本。原子批次按输入顺序执行 Add/Set/Delete，Add/Set 分配连续
revision；int64 溢出在算术前检测，绝对值上限即时执行，账户容量仅在批次末检查；
任何失败整体回滚。`Top` 按数值降序、名称升序，`Snapshot` 按名称排序。

## 多文件架构

- `creditpool.go` — 核心事务引擎：`New`/`Apply`/`Top`/`Snapshot` 与共享状态。
- `validation.go` — 无副作用的批次结构预检（`ValidateBatch`），`Apply` 复用同一套
  结构语义，保证“预检通过 ⇔ 事务不会因结构问题失败”。
- `stats.go` — 线性一致的状态统计（`Stats`），在读锁内一次性采样逻辑时钟与账户数。
- `clone.go` — 保留 generation/nextRevision 逻辑时钟的深拷贝（`Clone`）。

## 索引

账户存储为 `map[string]Account`，按名称 O(1) 定位。`Top` 与 `Snapshot` 在读取时
对物化切片排序，不维护有序索引——写路径保持 O(1)，避免每次事务更新堆/树。

## 候选事务

`Apply` 先完整结构预检（不读状态），再在写锁内把批次变更暂存到私有的 staged
副本（候选事务）上按序求值：溢出、绝对值上限、Delete 缺失、批次末容量任一失败都
直接返回，主状态未被触碰，回滚为零成本。全部通过后一次性提交并递增一次
generation（空批次不递增）。

## 所有权

所有公开方法在 `sync.RWMutex` 保护下执行；`Top`/`Snapshot` 返回的切片均为新建
副本，与内部状态隔离；`Clone` 逐条复制账户 map，克隆体与原账本互不影响。

## 复杂度

- `Apply`：O(k)，k 为批次内 op 数（另加最终账户数容量检查 O(k)）。
- `Top`：O(a log a)，a 为当前账户数。
- `Snapshot`：O(a log a)；`Stats`：O(1)；`Clone`：O(a)。
