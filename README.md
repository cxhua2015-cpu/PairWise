# shardbalance

并发安全的内存型分片负载账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，Add/Set 分配连续 revision，失败整体回滚。

## 索引与数据结构

- 主存储为 `map[string]Account`，按名称 O(1) 定位账户。
- `Top` 与 `Snapshot` 按需物化并排序：Top 按值降序、名称升序；Snapshot 按名称升序。
  未维护持久有序索引，以换取写入路径 O(1) 与实现的简洁性。

## 候选事务

`Apply` 先做完整结构校验（kind、名称字符集与长度），再在锁内把当前账户表
克隆为候选映射，所有操作作用于候选。int64 溢出在算术前检测，绝对值上限
（`MaxAbsValue`）在每步后立即检查，账户容量（`MaxAccounts`）仅在批次末检查。
任一步失败直接丢弃候选，已提交状态不受影响（整体回滚）；全部成功才用候选
替换主表，且非空成功批次 generation 只增加一次，revision 连续推进。

## 所有权与并发

- 所有公开方法由单个 `sync.Mutex` 保护，可安全并发调用。
- 返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建副本，
  与内部状态隔离，调用方可自由修改。
- `Account` 为纯值类型，按值拷贝，不存在共享指针。

## 复杂度

- `Apply`：O(B·A)（B 为批内操作数，A 为当前账户数，候选克隆主导）；校验 O(B·L)。
- `Top`：O(A log A)；`Snapshot`：O(A log A)。
- 空间：O(A)，候选事务期间临时 O(A)。
