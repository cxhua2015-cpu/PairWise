# taskqueue175

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计说明

### 索引

队列内部以 `map[string]Item` 作为按任务 ID 的哈希索引，Enqueue/Cancel 的
存在性判定为 O(1)。有序性不在写入路径维护：Pop 与 Snapshot 在读取时按
规范顺序（Priority 降序、ReadyAt 升序、ID 升序）对候选集临时排序。

### 候选事务

`Apply` 先对整个批次做纯结构校验（时间非负、kind 合法、ID 字符集与字节
上限、ReadyAt 非负），不读取任何队列状态；随后持锁检查单调时间，再顺序
执行 Enqueue/Cancel。每个操作记录一条 undo（Enqueue 记录待删除的 ID，
Cancel 记录待恢复的条目），revision 计数器的旧值一并保存。任一操作失败
或末尾容量检查（`len(items) > MaxItems`）失败时，逆序回放 undo 并恢复
revision，时间、状态、revision 一并回滚。非空成功批次 generation 恰好
加一，空批次不变。

### 所有权

所有公开方法通过同一把互斥锁串行化，可任意并发调用。`Pop` 返回的条目已
从队列原子删除；`Snapshot` 返回的切片是新分配的副本，调用方修改返回值
不会影响队列内部状态。

### 复杂度

- `New`：O(1)。
- `Apply`（k 个操作）：结构校验 O(k·L)（L 为 ID 长度），执行与容量检查
  O(k)，回滚（仅失败时）O(k)。
- `Pop`（n 个任务）：筛选 O(n)，排序 O(r log r)（r 为就绪任务数），删除
  O(limit)。
- `Snapshot`：O(n log n)，含一次完整拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
