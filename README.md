# readyqueue360

并发安全的内存型“就绪优先队列”，语义见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

### 索引
- 主存储为 `map[string]Item`，按 ID O(1) 定位，用于 Enqueue 的重复检测与 Cancel 删除。
- 不维护持久堆；Pop/Snapshot 时按需收集候选并排序（Priority 降序、ReadyAt 升序、ID 升序）。队列规模受 `MaxItems` 限制，按需排序比维护堆的代码更简单且足够快。

### 候选事务
- `Apply` 先做完整结构校验（时间非负、kind 合法、ID 字符集与字节上限），不读取任何状态。
- 校验通过后记录 `now`/`nextRevision` 检查点，顺序应用 Enqueue/Cancel，并为每步记录逆操作（undo）。
- 任一步失败（`ErrExists`/`ErrNotFound`）或末尾容量检查失败（`ErrCapacity`）时逆序回放 undo 并恢复检查点，实现时间、状态、revision 的完整回滚。
- 非空成功批次 `generation` 恰好加一；空批次直接返回，不改变任何状态。

### 所有权
- 所有公开方法由同一把 `sync.Mutex` 保护，可并发调用。
- `Pop` 与 `Snapshot` 返回的切片均为新建副本，调用方修改不会影响队列内部状态。

### 复杂度（n = 当前元素数，k = 批次内 op 数，m = 就绪元素数）
- `New`：O(1)。
- `Apply`：O(k) 均摊（map 操作 + 末尾容量检查），回滚同为 O(k)。
- `Pop`：O(n + m log m)，原子地选出并删除前 `limit` 个就绪元素。
- `Snapshot`：O(n log n)（排序输出规范顺序）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
