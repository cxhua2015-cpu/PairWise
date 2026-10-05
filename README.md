# migrationqueue

并发安全的内存型迁移优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

**索引**
- 主索引为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 去重（`ErrExists`）与 Cancel 查找（`ErrNotFound`）。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）在 `Pop`/`Snapshot` 时对候选切片用 `slices.SortFunc` 物化，不为读路径维护额外堆结构；队列规模受 `MaxItems` 限制，排序开销可控。

**候选事务**
- `Apply` 先做整批结构校验（kind、ID 字符集/长度、Cancel 不得携带 Priority/ReadyAt、非负时间），不触碰状态。
- 通过校验后在锁内克隆一份候选 map，顺序执行 Enqueue/Cancel，revision 从 `nextRevision` 起单调分配；最终容量只在末尾检查（`ErrCapacity`）。
- 任一步失败即丢弃候选，时间、条目与 revision 游标全部保持不变（回滚）；全部成功才一次性交换 map、推进 `now` 并将 generation 加一（空批次 generation 不变）。

**所有权**
- 所有公开方法共用一把 `sync.Mutex`，可任意并发调用。
- `Item` 为纯值类型，`Pop`/`Snapshot` 返回的切片均为新建副本，调用方修改不会影响内部状态。
- 时间为显式非负单调游标：`Apply.Now`/`Pop(now)` 小于当前时间返回 `ErrTime`，负时间或非法 limit 返回 `ErrInvalidInput`；成功调用推进游标，失败调用不变。

**复杂度**（n = 当前条目数，k = 批次内 op 数）
- `New`：O(1)。
- `Apply`：O(n + k)，克隆候选 map 加顺序执行；校验 O(k)。
- `Pop`：O(n log n)，筛选就绪项后排序并原子删除。
- `Snapshot`：O(n log n)，拷贝并排序。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
