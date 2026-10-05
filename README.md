# taskqueue180

并发安全的内存型任务优先队列，语义见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

### 索引

主索引是 `map[string]*Item`，按 ID 提供 O(1) 的存在性判定（`ErrExists`/`ErrNotFound`）
与 Cancel 定位。Pop/Snapshot 需要的规范顺序（Priority 降序、ReadyAt 升序、ID 升序）
在读取时对待命候选即时排序产生，不维护冗余有序结构，因此写路径保持 O(1)，
且不存在多索引不一致的风险。

### 候选事务

`Apply` 把每个批次视为一个候选事务：先在锁外做纯结构校验（kind、ID 字符集与字节
上限、非负时间），再在单把 `sync.Mutex` 内按序执行 Enqueue/Cancel。每个操作记录一条
逆向 undo（Enqueue 的逆操作是删除，Cancel 的逆操作是恢复原指针），同时保存进入批次
前的 `now`/`generation`/`nextRevision`。任何失败（含末尾才做的容量检查）按逆序回放
undo 并还原计数器，实现时间、状态与 revision 的完整回滚。容量只在批次末尾检查，
因此同一批次内“先 Cancel 再 Enqueue”可以越过瞬时超容。

### 所有权

内部 `*Item` 永不逃逸：`Pop` 与 `Snapshot` 返回的都是值拷贝切片，调用方修改返回
数据不会影响队列内部状态。Item 入队后不可变，回滚只需移动指针，无需深拷贝。

### 复杂度

- `Apply`：O(m)，m 为批次内操作数（map 操作均摊 O(1)，回滚同为 O(m)）。
- `Pop`：O(n log n)，n 为当前条目数（过滤 ReadyAt <= now 后排序，取前 k 个并删除）。
- `Snapshot`：O(n log n)（拷贝并排序）。
- 空间：O(n)。

所有公开方法持有同一把互斥锁，可安全并发调用；时间显式非负且单调递增，
空批次不改变 generation，非空成功批次 generation 恰好加一。
