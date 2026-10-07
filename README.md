# readyqueue400

并发安全的内存型“就绪优先队列”。详见 `SPEC.md`。

## 索引

- 主索引：`map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重与 Cancel 删除。
- 就绪视图：不维护持久堆；`Pop`/`Snapshot` 时现算候选集并按规范顺序
  （Priority 降序、ReadyAt 升序、ID 升序）排序，保证顺序唯一确定。

## 候选事务

`Apply` 是一个全有或全无的批次事务：

1. 先校验时间（非负、单调）与全部 Op 的结构（kind、ID 字符集与字节上限），
   结构校验通过前不读取/修改任何状态。
2. 顺序执行 Enqueue/Cancel，Enqueue 分配递增 revision，同时记录 undo 日志。
3. 任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）
   时按 undo 日志逆序回滚：恢复 map、回收已分配的 revision，时间与
   生成代（generation）保持不变。
4. 仅当整个批次成功时才推进 `now` 并将 generation 加一（空批次不变）。

`Pop` 同样是事务性的：校验失败（`ErrInvalidInput`/`ErrTime`）时不改变任何
状态；成功时原子删除所选任务并推进 `now`。

## 所有权

- 队列独占内部 `items` map；`Item` 为纯值类型，按值拷贝进出。
- `Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改不影响内部状态。
- 所有公开方法通过单一 `sync.Mutex` 串行化，支持任意并发调用。

## 复杂度

设 n 为队列中的任务数，b 为批次中的操作数，k 为 Pop 的 limit：

- `New`：O(1)。
- `Apply`：O(b) 校验与执行，O(b) 回滚（仅失败时）。
- `Pop`：O(n log n) 排序候选，删除 O(k)。
- `Snapshot`：O(n log n)。
- 空间：O(n)。
