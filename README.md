# readyqueue375

并发安全的内存型“就绪优先队列”，实现见 `readyqueue375/prioritybox.go`，规范见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Item`（ID → 条目），Enqueue/Cancel 按 ID O(1) 定位。
- 不维护有序堆；Pop/Snapshot 时现取候选并按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。条目数为 n 时这是 O(n log n)，对本队列的容量上限场景足够简单且正确。

### 候选事务（Apply）
- Apply 先做整批结构校验（kind、ID 字符集与字节上限、非负 ReadyAt、非负且单调的 Now），再读取状态。
- 执行时在 `maps.Clone` 出的候选副本上顺序应用 Enqueue/Cancel，Enqueue 从候选 `nextRevision` 起分配 revision；容量只在末尾检查（`len > MaxItems` 即 `ErrCapacity`）。
- 任一步失败直接丢弃候选副本，时间、条目、revision、generation 全部天然回滚；成功才一次性提交，非空批次 generation 恰好加一，空批次不变。

### 所有权与并发
- 所有公开方法由单把 `sync.Mutex` 串行化，支持任意并发调用。
- 返回的 `[]Item`/`Snapshot.Items` 均为新建切片与值拷贝，调用方修改不影响内部状态；队列不保留调用方传入的切片。

### 复杂度
- `New`：O(1)；`Apply`（k 个 op、当前 n 项）：O(n + k)；`Pop`：O(n log n)；`Snapshot`：O(n log n)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
