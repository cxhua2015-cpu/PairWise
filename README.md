# readyqueue275

并发安全的内存型“就绪优先队列”，Go 1.22+，仅依赖标准库。语义见 `SPEC.md`。

## 架构（多文件联动）

- `prioritybox.go` — 核心事务引擎：`New` / `Apply` / `Pop` / `Snapshot`。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`，`Apply` 复用同一套校验。
- `stats.go` — 线性一致的 `Stats` 摘要。
- `clone.go` — 保留逻辑时钟、所有权完全隔离的 `Clone`。

## 索引

- 主索引为 `map[string]Item`：Enqueue/Cancel 的存在性判断与删除均为 O(1)。
- `Pop` 在候选事务内物化 `ReadyAt <= now` 的就绪项，按 **Priority 降序、ReadyAt 升序、ID 升序** 排序后取前 n 个并原子删除。
- `Snapshot` 返回按 ID 排序的确定性副本，便于测试与比较。

## 候选事务

`Apply` 先调用共享的结构预检（kind、ID 字符集与字节上限、非负时间、Cancel 无负载字段），再检查单调时钟，然后在当前状态的**私有副本**上顺序执行全部 op，revision 在局部计数器上分配；最终容量只在末尾检查。任一步失败直接丢弃副本——时间、状态、revision 天然回滚，无需补偿日志。全部成功才一次性提交，非空成功批次 generation 恰好 +1，空批次不变。

## 所有权与并发

- 所有公开方法由同一把互斥锁串行化，`Stats`/`Snapshot`/`Clone` 因此都是线性一致的单点视图。
- `Snapshot`/`Pop` 返回的切片与 `Clone` 的 map 均为新建内存，调用方修改不会影响队列；`Clone` 复制 generation/nextRevision/now，revision 序列延续而非重启，克隆体与原队列互不影响。

## 复杂度

| 操作 | 时间 | 备注 |
| --- | --- | --- |
| `New` / `Stats` | O(1) | |
| `ValidateBatch` | O(k) | k 为批内 op 数，纯结构校验 |
| `Apply` | O(n + k) | n 为当前元素数（复制候选副本） |
| `Pop` | O(n log n) | 物化并就绪项排序 |
| `Snapshot` / `Clone` | O(n log n) / O(n) | Snapshot 含按 ID 排序 |

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
