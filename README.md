# readyqueue290

Read `SPEC.md` and implement the package.

## Multi-file architecture

The implementation is intentionally split across the core transaction engine, side-effect-free validation, linearizable statistics, and ownership-safe cloning. All four components are required by the public contract.


## 设计说明

### 索引

队列内部使用 `map[string]Item` 作为唯一索引，按 ID 提供 O(1) 的 Enqueue 去重与 Cancel 定位。规范弹出顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外堆结构，而是在 `Pop`/`Snapshot` 时对候选集即时排序物化，使写路径保持廉价且实现简单。

### 候选事务

`Apply` 先做与 `ValidateBatch` 完全共享的无副作用结构预检（非负时间、合法 kind、Cancel 不得携带 Priority/ReadyAt、ID 字符集与字节上限），再在互斥锁内检查单调时间，然后把整批操作顺序应用到一份候选 map 上：Enqueue 分配递增 revision，Cancel 删除条目，最终容量只在末尾检查。任一失败直接丢弃候选，时间、状态与 revision 全部回滚；成功才一次性提交，非空批次 generation 恰好加一。

### 所有权

所有公开方法持有同一把互斥锁，可并发调用。`Snapshot`/`Pop` 返回的切片均为新建副本，`Clone` 深拷贝全部条目与逻辑时钟（now、generation、nextRevision），克隆体与源队列不共享任何内存，互写不可见。

### 复杂度

- `New`/`Stats`：O(1)；`ValidateBatch`：O(L)，L 为批次长度。
- `Apply`：O(n + L)，n 为当前条目数（候选拷贝）。
- `Pop`/`Snapshot`：O(n log n)，源于候选集排序；`Clone`：O(n)。
