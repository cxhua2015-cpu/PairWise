# topiclog

并发安全的内存分区日志（Go 1.22+，仅标准库）。完整合同见 `SPEC.md`。

## 设计说明

### 索引与分区
- `Log` 持有 `topics map[string]*topic`，每个 topic 固定 `Partitions` 个分区；分区内记录按 offset 升序存放在切片中，配合 `base`（首条记录 offset-1）支持裁剪后的 O(1) 头部删除语义。
- 分区算法：空 key 进分区 0，否则 `fnv.New32a()` 的 FNV-1a 32 位哈希对分区数取模，由日志内部计算，调用方无法指定。
- 消费组提交存放在 `commits map[{group,topic,partition}]uint64`。

### offset 分配
- 每个 topic/partition 独立的高水位 `high`，从 1 开始严格连续递增；裁剪只删记录、不回退高水位，offset 永不重用。

### 事务（批量原子性）
- `AppendBatch`/`CommitBatch` 分三阶段：先对全部输入做结构校验（名称、key 长度、payload 大小、分区范围），再在持锁状态下按输入顺序执行语义操作（offset 分配、regression/ahead 检查），最后仅对追加批检查最终容量（topic 数、记录数、payload 字节）。
- 任一步失败通过 undo 日志整体回滚（追加逆序弹出、恢复高水位与用量、删除新建 topic；提交恢复旧值），状态与 generation 零副作用。
- 空批成功返回当前 generation；非空成功批 generation 恰好加一。

### 容量
- 容量只在追加批的最终状态检查：`len(topics) <= MaxTopics`、`usedRecords <= MaxRecords`、`usedPayload <= MaxPayloadBytes`，超限返回 `ErrCapacity` 并回滚。`usedRecords`/`usedPayload` 随追加与裁剪实时维护。

### 安全裁剪
- `Trim` 要求 topic 存在（否则 `ErrNotFound`），且至少一个消费组曾提交该 topic/partition、且所有这样的已知组 offset 均 `>= through`，否则 `ErrUnsafeTrim`。成功后删除 `Offset <= through` 的记录并更新用量；即使没删记录 generation 也加一。

### 所有权隔离
- 追加时 payload 深拷贝两份：一份存入内部状态，一份放入返回的 `Record`；`Read`/`Snapshot` 返回的记录同样深拷贝。调用方修改输入或返回值均不影响内部状态，内部状态也不别名任何外部切片。

### 并发与复杂度
- 单把 `sync.RWMutex`：写操作（Append/Commit/Trim）独占，读操作（Read/Snapshot）共享，全部公开方法可并发调用并通过 `-race`。
- 实际复杂度（n=批大小，r=分区记录数，t=topic 数，c=提交数）：
  - `AppendBatch`：O(n) 校验 + O(n) 追加（均摊）+ O(n) 回滚（仅失败时）。
  - `Read`：O(log r) 二分定位 + O(limit) 拷贝。
  - `CommitBatch`：O(n)；`Trim`：O(c) 安全性扫描 + O(log r) 定位 + O(r) 切片收缩。
  - `Snapshot`：O(t log t + 总记录数 + c log c)，含全部深拷贝。
