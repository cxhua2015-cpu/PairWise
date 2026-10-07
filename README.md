# readyqueue320

并发安全的内存型“就绪优先队列”，语义见 `SPEC.md`。仅依赖标准库，Go 1.22+。

## 设计说明

### 索引
- 主索引为 `map[string]Item`，按 ID 保证唯一性，Enqueue/Cancel 的查找为 O(1)。
- 不维护持久堆；Pop 时收集 `ReadyAt <= now` 的候选并按规范顺序
  （Priority 降序、ReadyAt 升序、ID 升序）排序后截取。队列容量受
  `Options.MaxItems` 约束，单次排序成本有界。

### 候选事务
- `Apply` 先对整个批次做完整结构校验（kind、ID 字符集与字节上限、
  非负时间），不读取任何状态；随后在时间单调性检查后，于主索引的
  **候选副本**上顺序执行 Enqueue/Cancel，revision 在候选上分配。
- 最终容量只在末尾检查（`len(candidate) > MaxItems` 即 `ErrCapacity`）。
- 任一 op 失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）即丢弃候选，
  时间、状态和 revision 计数器全部回滚；全部成功才一次性提交，
  非空批次 generation 恰好 +1，空批次不变。

### 所有权与并发
- 所有公开方法由一把 `sync.Mutex` 串行化，可任意并发调用；
  Pop 的选择与删除在锁内原子完成。
- `Pop`/`Snapshot` 返回的切片与 `Item` 值均为新分配的副本，
  调用方修改不影响队列内部状态；队列不保留调用方传入切片的引用。

### 复杂度
设 n 为队列中项数，k 为批次数：
- `Apply`：结构校验 O(k)，候选复制 O(n)，执行 O(k)，合计 O(n + k)。
- `Pop`：筛选 O(n)，排序 O(n log n)，删除 O(limit)。
- `Snapshot`：O(n log n)（排序为规范顺序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
