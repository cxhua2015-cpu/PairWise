# resourcecatalog146

并发安全的内存型“资源目录 146”，Go 1.22+，仅标准库。三层结构：

- `resourcecatalog146/servicecatalog.go`：状态引擎，持有事务数据与快照。
- `resourcecatalog146/policy.go`：准入策略，独立同步、可原子替换的 actor 白名单 + 单批操作数上限。
- `resourcecatalog146/coordinator.go`：协调层，先授权再调用引擎，并为成功/拒绝/引擎失败记录连续审计序号。

## 索引

状态引擎使用 `map[string]entry` 作为主索引（`entry{value, revision}`），配一把 `sync.Mutex` 保护 map、`generation` 与 `nextRevision`。`Snapshot`/`Result.Changed` 按需对名称排序（`sort.Strings`），不维护额外有序结构。

## 候选事务

`Apply` 先在无锁状态下做完整结构校验（kind、名称字符集与长度、Value 长度、Delete 不得携带 Value），随后持锁把当前 map 浅拷贝为候选事务，按输入顺序回放 Put/Delete：Put 从 `nextRevision` 起分配连续 revision，Delete 不分配且要求键存在（否则 `ErrNotFound`）。记录数与 Value 总字节容量只在批次末检查；任何失败直接丢弃候选，状态、generation、revision 全部不变。非空成功批次 generation 只加一，空批次不变。

## 所有权

- 写入时复制 `Op.Value`，`Get`/`Snapshot`/`Result.Changed` 返回深拷贝，调用方后续修改不影响内部状态。
- `Policy` 的白名单是不可变 map，通过 `atomic.Pointer` 整体替换，读者无锁。
- `Coordinator.Decisions()` 返回新切片拷贝，不别名内部审计日志。

## 复杂度

- `Apply`：校验 O(L)，候选拷贝 O(N)，回放 O(L)，末检 O(N)，排序变更 O(K log K)；L 为批内字节/操作量，N 为记录数，K 为触及键数。
- `Get` O(1)（均摊），`Snapshot` O(N log N)，`Authorize` O(1)，审计追加 O(1) 均摊。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
