# resourcecatalog086

并发安全的内存型资源目录，实现 `SPEC.md` 中的原子批次语义。仅依赖标准库，需 Go 1.22+。

## 设计说明

### 索引
- 主索引为两张并行哈希表：`map[string][]byte`（名称 → Value）与 `map[string]uint64`（名称 → Revision），另维护 `totalVal`（Value 总字节）与 `nextRev`（下一个待分配 revision，从 1 开始）两个计数器。
- 查询按名称 O(1)；`Snapshot` 与 `Result.Changed` 在返回前对名称排序，因此不额外维护有序结构。

### 候选事务（candidate transaction）
- `Apply` 先在持锁状态下对整个批次做**完整结构校验**（kind 合法、名称字符集/长度、Value 长度、Delete 不得携带 Value），任一失败直接返回 `ErrInvalidInput`，不读取任何状态。
- 校验通过后，把当前记录、revision 表和字节总量克隆为候选状态，按输入顺序在候选上执行 Put/Delete：Put 分配连续 revision，Delete 不分配；Delete 缺失名称即返回 `ErrNotFound`。
- 记录数与 Value 总字节容量**只在批次末**检查，超限返回 `ErrCapacity`。
- 任何失败都发生在候选上，直接丢弃——真实状态、generation、revision 天然回滚，无需逆操作。提交时整体换入候选，非空成功批次 generation 恰好 +1，空批次不变。

### 所有权
- Put 的 Value 在入库时深拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝副本，调用方对返回切片的修改不影响内部状态，反之亦然。

### 并发与复杂度
- 所有公开方法由单把 `sync.Mutex` 串行化，可安全并发调用；读写均为短临界区。
- 复杂度：`Apply` 为 O(n + m log m)（n 为批次内 op 数，m 为触及名称数，另加一次与现存记录数成正比的候选克隆）；`Get` O(1)；`Snapshot` O(k log k)（k 为记录数）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
