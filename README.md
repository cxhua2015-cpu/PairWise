# readyqueue310

Read `SPEC.md` and implement the package.

## 实现说明

### 索引

队列内部以 `map[string]Item` 作为 ID 主索引，Enqueue/Cancel 按 ID O(1) 定位；
`Item` 内联存储 `Priority`、`ReadyAt`、`Revision`，无额外堆结构。Pop 与 Snapshot
时按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）对候选切片即时排序，避免维护
多个索引之间的一致性开销。

### 候选事务

`Apply` 先在持锁状态下把当前 map 浅拷贝为候选副本，按顺序在副本上执行
Enqueue/Cancel 并推进本地 revision 计数；全部成功且最终容量（仅在末尾检查一次）
不越限时才整体提交：替换 map、推进单调时间、generation 加一、提交 nextRevision。
任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃副本，时间、状态与
revision 全部保持不变，实现原子回滚。批次在做任何状态读取前先完成完整结构校验
（kind、ID 字符集与字节上限、ReadyAt 非负）。

### 所有权

所有公开方法共用一把 `sync.Mutex`，可并发调用。`Pop` 与 `Snapshot` 返回的切片均为
新分配的副本，`Item` 为值类型，调用方对返回值的修改不会影响队列内部状态。

### 复杂度

- `New`：O(1)。
- `Apply`（k 个 op、当前 n 项）：候选拷贝 O(n)，执行 O(k)，总计 O(n + k)。
- `Pop`（n 项中 r 项就绪）：筛选 O(n)，排序 O(r log r)，删除 O(min(r, limit))。
- `Snapshot`：O(n log n)（拷贝并规范排序）。
