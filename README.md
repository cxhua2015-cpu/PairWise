# taskqueue130

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 索引

- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、入队、取消与删除。
- 不维护持久堆：`Pop`/`Snapshot` 时把候选拷贝到临时切片并按规范序
  （Priority 降序、ReadyAt 升序、ID 升序）排序。队列规模受 `MaxItems`
  上限约束，按需排序比维护堆 + 懒删除更简单且无正确性风险。

## 候选事务（Apply）

`Apply` 先在持锁前做整批结构校验（kind、ID 字符集与字节上限、ReadyAt 非负），
不读取任何状态；随后持锁检查单调时间，再按序执行 Enqueue/Cancel：

- 每个操作记录一条 undo（入队的 ID 或被取消的 Item 快照），并保存
  进入批次前的 `nextRevision`。
- 任一操作失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败
  （`ErrCapacity`）时，逆序回放 undo 并恢复 revision 计数器，
  时间与 generation 因尚未提交而天然不变——即“失败回滚时间、状态和 revision”。
- 容量只在批次末尾检查，因此同批内“先取消再入队”可以成功。
- 全部成功才提交：推进 `now`、非空批次 `generation` 恰好加一。

## 所有权与并发

- 所有公开方法由同一把 `sync.Mutex` 保护，可任意并发调用；
  结构校验在锁外进行，缩短临界区。
- `Item` 为值类型，map 中存副本；`Pop`/`Snapshot` 返回新建的切片，
  调用方对返回值的修改不影响内部状态（返回切片与内部状态隔离）。
- 时间为显式非负单调值：负值返回 `ErrInvalidInput`，回退返回
  `ErrTime`；`Apply` 与 `Pop` 成功时推进队列时钟。

## 复杂度

设 n 为队列中任务数，k 为批次操作数，r 为就绪任务数，m 为 Pop 上限：

- `New`：O(1)。
- `Apply`：校验 O(k·L)（L 为 ID 长度），执行 O(k)，末尾容量检查 O(1)；
  回滚代价与已执行操作数成正比，O(k)。
- `Pop`：筛选 O(n)，排序 O(r log r)，删除 O(min(r, m))。
- `Snapshot`：O(n log n)，返回按规范序排列的副本。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
