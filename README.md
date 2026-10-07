# readyqueue325

并发安全的内存型“就绪优先队列”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 设计说明

### 索引

- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的查找、入队与取消。
- 不维护持久化堆；`Pop` 与 `Snapshot` 在持锁状态下即时收集候选并排序，规范顺序为 Priority 降序、ReadyAt 升序、ID 升序。队列规模受 `MaxItems` 上限约束，因此按需排序比维护额外堆结构更简单且足够快。

### 候选事务（Apply）

- `Apply` 先做整批结构校验（kind、ID 字符集与长度、非负时间），不读取任何状态；随后检查时间单调性。
- 校验通过后按序执行 Enqueue/Cancel，Enqueue 从单调计数器分配 revision；容量只在批次末尾检查一次。
- 每个已执行的 op 记录一条撤销记录（新增则删除、删除则恢复旧值）。任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）时逆序回放撤销记录，并回滚 revision 计数器与队列时间，generation 不变——批次要么整体生效，要么完全无副作用。
- 非空成功批次 generation 恰好加一；空批次只推进时间，不改变 generation。

### 所有权与并发

- 所有公开方法（`Apply`/`Pop`/`Snapshot`）共用一把 `sync.Mutex`，可任意并发调用；`Pop` 的选择与删除在同一临界区内原子完成。
- 返回值所有权归调用方：`Pop` 与 `Snapshot` 返回的切片均为新建副本，`Item` 为纯值类型，调用方修改返回值不会影响队列内部状态。

### 复杂度

- `Apply`：O(k)，k 为批内 op 数（末尾容量检查 O(1)）。
- `Pop`：O(m log m)，m 为当前就绪候选数（m ≤ MaxItems）。
- `Snapshot`：O(n log n)，n 为当前元素数。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
