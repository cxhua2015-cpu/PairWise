# taskqueue120

Read `SPEC.md` and implement the package.

## 设计说明

### 索引
队列内部以 `map[string]Item` 作为主索引，按任务 ID 提供 O(1) 的存在性判定、
Enqueue 查重（`ErrExists`）与 Cancel 定位（`ErrNotFound`）。规范顺序
（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序结构，而是在 `Pop` 与
`Snapshot` 时对该时刻的候选集排序生成，保证排序键定义只有一份（`less`）。

### 候选事务
`Apply` 采用候选事务（copy-on-write）：先对整个批次做完整结构校验
（`Now >= 0`、kind 合法、ID 字符集与字节上限、`ReadyAt >= 0`），再检查时间单调性，
随后在克隆的 map 与克隆的 `nextRevision` 上顺序执行 Enqueue/Cancel，容量只在
最后检查一次。任一环节失败直接丢弃克隆，时间、条目与 revision 计数天然回滚；
全部成功才一次性提交，非空批次 generation 恰好加一，空批次不变。

### 所有权
所有公开方法（`Apply`/`Pop`/`Snapshot`）由同一把 `sync.Mutex` 保护，可并发调用。
`Pop` 的选择与删除在同一临界区内原子完成。返回的切片（`Pop` 结果、
`Snapshot.Items`）均为新建副本，调用方修改不会影响队列内部状态，反之亦然。

### 复杂度
- `New`：O(1)。
- `Apply`：结构校验 O(B)，候选克隆与顺序执行 O(N + B)，末尾容量检查 O(1)，
  其中 B 为批次 op 数、N 为当前任务数。
- `Pop`：排序 O(N log N)，选取并删除至多 k 个就绪任务 O(N + k)。
- `Snapshot`：O(N log N)，含防御性拷贝。
- ID 校验 O(L)，L 为 ID 字节数，受 `MaxIDBytes` 上限约束。
