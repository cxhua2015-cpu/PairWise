# taskqueue130

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 索引与所有权

- **主索引**：`map[string]Item`，以任务 ID 为键，Enqueue/Cancel 的查找与写入为 O(1) 均摊。
- **所有权**：`Item` 为值类型，队列内部持有独立副本；`Pop`/`Snapshot` 返回的切片均为新建，调用方修改返回结果不会影响内部状态（返回切片与内部状态隔离）。
- **并发**：所有公开方法由同一把 `sync.Mutex` 保护，可安全并发调用；锁内无阻塞操作。

## 候选事务（Apply / Pop）

- **Apply**：先在锁外对整批 op 做完整结构校验（kind、ID 字符集与字节上限、非负 ReadyAt/Now），再持锁检查时间单调性，然后顺序执行 Enqueue/Cancel——Enqueue 分配递增 revision，Cancel 按 ID 删除——**最终容量只在末尾检查**（因此同批先 Cancel 再 Enqueue 可以净不换容量而成功）。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）通过逆序 undo 日志回滚条目、revision 计数与时间，状态与调用前完全一致。非空成功批次 generation 恰好 +1，空批次不变。
- **Pop**：持锁后收集 `ReadyAt <= now` 的候选任务，按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序，截取前 `limit` 个并在同一临界区内原子删除。时间只前进，回退返回 `ErrTime`；失败不产生任何副作用。

## 复杂度

| 操作 | 时间 | 说明 |
| --- | --- | --- |
| `New` | O(1) | 校验 Options（容量与 ID 字节上限必须为正） |
| `Apply`（k 个 op，n 个任务） | O(k) 均摊 | 结构校验 O(k·L)，L 为 ID 长度；回滚 O(k) |
| `Pop` | O(n + r log r) | r 为就绪候选数；扫描 + 排序 + 删除 |
| `Snapshot` | O(n log n) | 复制并按规范顺序排序 |

## 使用

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
