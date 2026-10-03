# topiclog

并发安全的内存分区日志（Go 1.22+，仅标准库）。完整合同见 `SPEC.md`。

## 设计说明

### 索引与分区
- 分区数在 `New` 时固定（1..64），所有 topic 共享。空 key 进入分区 0，否则用
  `hash/fnv` 的 FNV-1a 32 位对分区数取模，分区稳定且由日志内部计算。
- 每个 topic 持有定长分区数组；每个分区只保存一个按 offset 升序、连续的
  `[]Record` 切片，外加 `base`（已裁剪水位）与 `high`（历史最高 offset）。
  分区内第 i 条记录的 offset 即 `base+1+i`，无需额外索引结构。

### offset 分配
- 每个 topic/partition 的 offset 从 1 开始严格连续递增；裁剪只推进 `base`，
  `high` 永不回退，因此 offset 绝不重用。

### 事务（批量原子性）
- `AppendBatch`/`CommitBatch` 先对整批做结构校验（名称、key 长度、payload 大小、
  分区范围），再在隔离的候选副本上按输入顺序执行语义操作（追加、回归/超前检查）。
- 容量检查（topic 数、记录数、Payload 字节）只在候选最终状态上做一次。
- 任一步失败直接丢弃候选，零副作用；成功才整体换入并将 generation 推进一次。
  空批成功返回当前 generation，不推进。

### 容量
- `MaxTopics`、`MaxRecords`、`MaxPayloadBytes` 为全局限额，按批后最终状态判定，
  超限返回 `ErrCapacity` 并整体回滚。`Trim` 同步扣减记录与字节用量。

### 安全裁剪
- `Trim` 要求 topic 存在（否则 `ErrNotFound`），且至少一个消费组曾对该
  topic/partition 提交、且所有这样的已知组 offset 均 `>= through`，否则
  `ErrUnsafeTrim`。成功后删除 `Offset <= through` 的记录；即使没删记录，
  generation 也加一。

### 所有权隔离
- 输入 Payload 在追加时深拷贝；`Read`/`Snapshot` 返回的记录与 Payload 均为
  深拷贝，调用方对返回值的修改不会影响内部状态，反之亦然。

### 并发与复杂度
- 全部公开方法由单个 `sync.Mutex` 串行化，可通过 `-race` 检测。
- `AppendBatch`：O(B + T)（B 为批大小，T 为现有 topic 数，候选复制仅拷贝分区
  元数据与切片头，记录切片共享、提交后不可变）。
- `Read`：O(limit)，利用连续性 O(1) 定位起点。
- `CommitBatch`：O(B + C)（C 为现有提交数）。
- `Trim`：O(C + K)（C 为提交数，K 为保留记录数，拷贝保留段以释放被裁剪内存）。
- `Snapshot`：O(总记录数 + 提交数)，输出按 Topic / Partition / Offset 及
  Group、Topic、Partition 排序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
