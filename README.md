# auctionqueue

并发安全的内存型“竞价优先队列”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、插入与删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序结构，`Pop` 与 `Snapshot` 按需对候选切片排序，以换取写入路径的 O(1) 与实现的简洁。

### 候选事务
- `Apply` 先做整批结构校验（Kind 合法、ID 字符集与字节上限、ReadyAt/Now 非负），再加单把 `sync.Mutex` 串行执行。
- 执行期间记录 undo 日志（被覆盖/删除的旧值、新增键、revision 增量）；任一操作失败或末尾容量检查失败时，逆序回放 undo，完整回滚状态、时间与 revision，批次不产生任何可见副作用。
- 容量（`MaxItems`）只在批次末尾检查，因此同批次内先 Cancel 再 Enqueue 不会触发 `ErrCapacity`。
- 成功的非空批次 `generation` 恰好 +1，`now` 单调推进；空批次只推进时间，不改变 generation。

### 所有权
- 队列不保留调用方传入的 `Batch`/`Op` 切片；返回的 `[]Item`（`Pop`）与 `Snapshot.Items` 均为新分配的副本，调用方可自由修改，内部状态与之隔离。
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）持有同一把互斥锁，可安全并发调用；`-race` 下无数据竞争。

### 复杂度
设批次长度为 k、队列中元素数为 n：
- `New`：O(1)。
- `Apply`：结构校验 O(k·L)（L 为 ID 字节数），执行 O(k)，容量检查 O(1)；回滚仅发生在失败路径，O(k)。
- `Pop`：筛选就绪候选 O(n)，排序 O(r log r)（r 为就绪数），删除 O(r)。
- `Snapshot`：O(n log n)，并复制全部条目。
