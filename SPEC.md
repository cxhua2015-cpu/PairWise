# topiclog contract

## Options 与输入边界

- `Partitions` 1..64，所有 topic 使用相同分区数；`MaxTopics` 1..10000，`MaxRecords` 1..1,000,000，`MaxPayloadBytes` 1..64 MiB。
- Topic 与 Group 长度 1..64 字节，只允许 ASCII 字母、数字、点、下划线、连字符；Key 最多 256 字节；单个 Payload 最多 1 MiB。
- Key 为空时进入分区 0；否则使用标准库 `hash/fnv` 的 FNV-1a 32 位结果对 Partitions 取模。分区由日志计算，调用方不能指定。

## AppendBatch 与 Read

- 空追加批成功返回当前 generation。非空批先按输入顺序完成全部结构校验，再在候选副本按输入顺序追加。
- 每个 topic/partition 的 offset 从 1 开始严格连续；裁剪不重用 offset。批中首次出现 topic 时创建 topic。
- 全批完成后检查最终 topic 数、记录数、Payload 总字节；失败零副作用。非空成功批 generation 加一。
- `Read(topic,partition,after,limit)` 要求合法 topic、partition 范围内、limit 1..1000；未知 topic 成功返回空结果。返回该分区中 `Offset > after` 的前 limit 条，按 offset 升序，并带当前 generation。

## 消费组提交

- `CommitBatch` 空批返回当前 generation。非空批先结构校验所有 Group/Topic/Partition，再在候选副本按顺序执行。
- Offset 可为 0；提交必须不小于同组现值，否则 `ErrOffsetRegression`；不得大于对应 topic/partition 已分配的最高 offset，否则 `ErrOffsetAhead`。未知 topic 的最高 offset 为 0。
- 任一步失败整体回滚；非空成功批 generation 加一，即使提交值等于原值。

## 安全裁剪

- `Trim(topic,partition,through)` 先校验 topic 与 partition。未知 topic 返回 `ErrNotFound`。
- 只有至少一个消费组曾提交该 topic/partition，且每个这样的已知组 offset 都 `>= through` 时才安全；否则返回 `ErrUnsafeTrim`。`through` 可为 0 或超过最高 offset。
- 成功删除该分区中 `Offset <= through` 的记录，更新记录与 Payload 容量；generation 加一，即使没有记录被删。最高 offset 不回退。

## Snapshot、排序与所有权

- Snapshot Topics 按 Topic 升序；每个 Topic 的 Partitions 按编号升序，Records 按 offset 升序；Commits 按 Group、Topic、Partition 排序。
- 所有输入 Payload 以及 Read/Snapshot 返回 Payload 必须与内部状态及彼此隔离。
- 所有公开方法可被多个 goroutine 并发调用并通过 race detector。操作级错误可包装 sentinel，但 `errors.Is` 必须成立。
