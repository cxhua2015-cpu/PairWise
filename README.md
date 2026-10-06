# readyqueue220

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队与取消。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护额外有序索引：`Pop` 与 `Snapshot` 在持锁状态下对候选集即时排序，避免双索引一致性开销。

### 候选事务
- `Apply` 分两阶段：先对整个批次做纯结构校验（kind、ID 字符集与长度、时间非负且单调），不读取任何状态；通过后在**克隆的 map 与 revision 计数器**上顺序执行 Enqueue/Cancel。
- 任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时直接丢弃候选状态，队列的时间、条目、revision、generation 全部保持不变，天然实现回滚。
- 全部成功才一次性提交：替换 map、推进时间、提交 revision 计数；非空批次 generation 恰好 +1，空批次不变。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可任意并发调用；`Apply` 内多 op 与 `Pop` 的选择+删除均原子完成。
- `Snapshot` 与 `Pop` 返回的切片均为新分配的副本，调用方修改返回值不会影响队列内部状态。

### 复杂度
- `Apply`：O(k·n)（k 为批大小，n 为当前条目数，来自候选克隆）；校验与执行本身为 O(k)。
- `Pop`：O(n log n)，仅就绪条目（ReadyAt <= now）参与排序，删除为 O(min(n, 返回数))。
- `Snapshot`：O(n log n)。空间 O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
