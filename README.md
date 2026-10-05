# taskqueue165

并发安全的内存型任务优先队列（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 设计

### 索引
- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判断、入队与取消。
- 不维护有序堆：Pop 与 Snapshot 时按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）对候选即时排序。队列规模受 `MaxItems` 上限约束，排序开销可控，且实现无堆修复路径，易于保证事务性。

### 候选事务（Apply）
1. 先做整批结构校验（kind、ID 字符集与字节上限、ReadyAt 非负、Now 非负且单调），任何失败在触碰状态前返回。
2. 将 `items` 复制为候选 map，revision 计数器复制为局部变量，顺序执行 Enqueue/Cancel。
3. 容量检查只在所有操作执行完后进行一次（`len(candidate) > MaxItems` → `ErrCapacity`）。
4. 任一步失败直接返回，队列的时间、条目与 revision 全部保持原值（回滚通过“不提交”实现）；全部成功才一次性提交，generation 恰好加一，revision 只增不回收。

### 所有权与并发
- 所有公开方法由同一把 `sync.Mutex` 保护，可任意并发调用。
- `Item`/`Snapshot` 为值语义；`Snapshot.Items` 与 `Pop` 返回值均为新分配的切片，调用方修改不影响内部状态。
- Pop 在锁内选择并原子删除最多 n 个 `ReadyAt <= now` 的条目，同时推进队列时间。

### 复杂度
- `Apply`：O(k·n) 复制候选 map（k 为批大小，n 为当前条目数，n ≤ MaxItems），校验 O(k·L)（L 为 ID 长度）。
- `Pop`：O(n log n) 排序候选，实际候选仅为 ready 条目。
- `Snapshot`：O(n log n)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
