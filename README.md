# readyqueue340

并发安全的内存型“就绪优先队列”。语义见 `SPEC.md`。

## 设计说明

- **索引**：队列主体为 `map[string]Item`，以 ID 为键提供 O(1) 的存在性判定、
  入队、取消与删除；`Snapshot`/`Pop` 时按需按规范顺序
  （Priority 降序、ReadyAt 升序、ID 升序）排序。
- **候选事务（candidate transaction）**：`Apply` 先在互斥锁内完成整批结构校验
  （kind、ID 字符集与字节上限、非负时间），再把当前 map 浅拷贝到暂存区，
  在暂存区上顺序执行 Enqueue/Cancel 并推进 revision；容量只在全部操作
  执行完后检查一次。任何一步失败直接丢弃暂存区，时间、状态、revision
  与 generation 天然回滚；成功则一次性换入暂存区，空批次不增加 generation。
- **所有权**：所有公开方法共用一把 `sync.Mutex`，可并发调用；`Pop` 与
  `Snapshot` 返回的切片均为新分配的副本，调用方修改不影响内部状态。
  队列时间为显式非负单调时间，`Apply`/`Pop` 的 `now` 小于当前时间返回
  `ErrTime`，负数返回 `ErrInvalidInput`。
- **复杂度**：`Apply` 为 O(n + m)，n 为当前队列大小（暂存拷贝）、m 为批大小；
  `Pop`/`Snapshot` 为 O(n log n)（筛选/拷贝 + 排序）；单点查找/删除为 O(1)。
  空间 O(n)。
