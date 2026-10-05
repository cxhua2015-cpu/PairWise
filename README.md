# taskqueue090

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：内部使用 `map[string]Item` 以 ID 为键，Enqueue/Cancel 的查重与定位为 O(1)；规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 Pop/Snapshot 时按需排序得到，避免写入路径维护堆的复杂度。
- **候选事务**：`Apply` 先做整批结构校验（kind、ID 字符集与字节上限、Now 非负），再在单把互斥锁内顺序执行 Enqueue/Cancel，最后才检查容量。每个操作记录逆操作（enqueue→删除、cancel→回插），任一步失败或容量超限即逆序回滚，并恢复时间与 revision 计数，保证批次原子性。
- **所有权**：队列独占内部 map；`Snapshot` 与 `Pop` 返回新建切片，调用方修改不影响内部状态。所有公开方法由同一把 `sync.Mutex` 保护，可并发调用。
- **复杂度**：Apply 为 O(k)（k 为批内操作数，另加 O(1) 容量检查）；Pop 与 Snapshot 为 O(n log n)（n 为队列中任务数）；空间 O(n)。
- **时间/revision/generation**：时间为显式非负单调值，Now 回退返回 `ErrTime`；Enqueue 从 1 起分配单调 revision；非空成功批次 generation 恰好加一，空批次不变。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
