# readyqueue315

并发安全的内存型“就绪优先队列”，实现见 `readyqueue315/prioritybox.go`，语义以 `SPEC.md` 与契约测试为准。

## 设计说明

- **索引**：队列主体为 `map[string]Item`，按 ID O(1) 定位 Enqueue 去重与 Cancel；不存在单独的就绪堆，Pop/Snapshot 时对候选集按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。
- **候选事务**：`Apply` 先做整批结构校验（kind、ID 字符集与字节上限、非负时间），再在条目 map 的克隆上顺序执行 Enqueue/Cancel 并分配 revision，最终容量只在末尾检查；任一步失败直接丢弃克隆，时间、状态与 revision 天然回滚，成功才整体换入并令非空批次 generation 增一。
- **所有权**：所有公开方法由一把 `sync.Mutex` 保护，可并发调用；`Snapshot` 与 `Pop` 返回的切片均为新建副本，调用方修改不影响内部状态。
- **复杂度**：Enqueue/Cancel 单op均摊 O(1)（克隆整体为 O(n)，n 为当前条目数）；Pop 为 O(n + r log r)（r 为就绪数）；Snapshot 为 O(n log n)；空间 O(n)。
