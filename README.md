# readyqueue390

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队、取消与删除。
- 不维护持久化堆；Pop 与 Snapshot 时按需物化切片并排序，规范顺序为 Priority 降序、ReadyAt 升序、ID 升序。队列规模受 `MaxItems` 上限约束，线性扫描 + 排序的成本有界且实现简单、无堆修复路径。

### 候选事务（Apply）
- Apply 先做完整结构校验（kind、ID 字符集与字节上限、非负时间），再读取任何状态。
- 随后在“候选”覆盖表 `map[ID]staged` 上按序重放 Enqueue/Cancel：Enqueue 在候选上预分配 revision，Cancel 可抵消同批次先前的 Enqueue（净效果为空操作）或标记删除已存在项。
- 容量只在末尾检查一次（候选净效果应用到当前大小之后），因此批次内允许瞬时超容。
- 任一步失败直接返回，候选被丢弃——时间、状态、revision 计数器天然回滚，无需逆向补偿。成功才提交：应用候选、推进单调时间、generation 恰好 +1、提交 nextRevision。空批次为完全无操作，generation 不变。

### 所有权与并发
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，整个操作在临界区内原子完成，可任意并发调用。
- `Snapshot` 与 `Pop` 返回的切片均为新建副本，调用方对返回值（含 `Item`）的修改不会影响内部状态；`Item` 为纯值类型，不存在共享指针。
- 时间为显式非负单调值：负值返回 `ErrInvalidInput`，回退返回 `ErrTime`，失败操作不推进时间。

### 复杂度
设 n 为当前队列大小，b 为批次内操作数，k 为 Pop 的 limit：
- `New`：O(1)。
- `Apply`：O(b) 结构校验 + O(b) 候选重放 + O(b) 提交；空间 O(b)。
- `Pop`：O(n) 扫描就绪项 + O(r log r) 排序（r 为就绪数）+ O(k) 删除。
- `Snapshot`：O(n log n) 排序拷贝。
