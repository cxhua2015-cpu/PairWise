# resourcelease099

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：单索引设计。条目存于 `map[string]Entry`，键即租约名，查找/插入/删除均为 O(1) 均摊。过期不做堆索引，而是在每次 `Apply`/`Expire` 时按闭区间 `ExpiresAt <= Now` 全量扫描淘汰（O(n)），因为时间显式单调推进，扫描成本与状态规模成正比且实现简单、无额外一致性负担。
- **候选事务**：`Apply` 先在锁外对整个批次做结构校验（kind、键字符集与字节上限、非负 ExpiresAt），再在锁内检查时间单调性。随后在条目映射的**副本**上先淘汰过期条目、再顺序执行 Put/Touch/Delete（Put/Touch 分配递增 revision）。最终容量检查或任何中途错误发生时直接丢弃副本——淘汰、时间、generation 与 revision 随候选状态一起回滚，已提交状态不被触碰。非空成功批次 generation 只加一；空批次不改变 generation。
- **所有权**：`Table` 内部状态仅由持有 `sync.Mutex` 的公开方法访问，所有公开方法（`Apply`/`Expire`/`Snapshot`）可并发调用。`Snapshot` 与 `Expire` 返回的切片均为新建拷贝，调用方修改不影响表内状态；`Entry` 为纯值类型，无共享指针。
- **复杂度**：`Apply` 为 O(n + m)（n 为当前条目数，m 为批内操作数）；`Expire` 为 O(n)；`Snapshot` 为 O(n log n)（按字典序排序返回，保证确定性输出）；`New` 为 O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
