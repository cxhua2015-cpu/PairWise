# taskqueue110

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按任务 ID 提供 O(1) 的存在性判定、插入与删除。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时物化：先收集候选，再一次 `sort.Slice` 排序，不为排序序维护额外索引结构，避免双索引不一致风险。

### 候选事务
- `Apply` 先在持锁状态下做整批结构校验（kind、ID 字符集与字节上限、非负时间），再在主索引的克隆（候选 map）上顺序执行 Enqueue/Cancel。
- 仅在全部操作成功且**最终**容量检查通过后才原子提交：替换 map、推进 `now`、`nextRev` 并使 `generation` 恰好 +1。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity` 等）直接丢弃候选，时间、状态与 revision 完全回滚；空批次不改变任何状态。

### 所有权
- 所有公开方法（`Apply`/`Pop`/`Snapshot`）由一把 `sync.Mutex` 保护，可任意并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改返回值不会影响队列内部状态。
- `Pop` 的选择与删除在同一临界区内原子完成，不存在两个调用者弹出同一任务的窗口。

### 复杂度
- `Apply`：O(k·n) 克隆 + O(k) 执行（k 为批次大小，n 为队列大小）；校验 O(k·L)（L 为 ID 长度）。
- `Pop`：O(n log n)（筛选 + 排序 + 至多 limit 次 O(1) 删除）。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
